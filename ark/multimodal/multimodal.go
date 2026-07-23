package multimodal

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/internal/infra/llm"
)

// Attachment defines a multimodal file or image attachment for LLM requests.
type Attachment struct {
	ID         string         `json:"id,omitempty"`
	Filename   string         `json:"filename,omitempty"`
	MIMEType   string         `json:"mime_type,omitempty"`
	URL        string         `json:"url,omitempty"`
	DataBase64 string         `json:"data_base64,omitempty"`
	SizeBytes  int64          `json:"size_bytes,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// LLMConfig defines connection parameters for Domour LLM vision models.
type LLMConfig struct {
	Provider string `json:"provider"` // "openai", "gemini", "qwen", "llamacpp", "deepseek"
	APIKey   string `json:"api_key"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// NewImageAttachmentFromFile loads an image file from disk and encodes it into a Base64 data attachment.
func NewImageAttachmentFromFile(path string) (*Attachment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read image file %s: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		switch ext {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".webp":
			mimeType = "image/webp"
		default:
			mimeType = "image/jpeg"
		}
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	return &Attachment{
		Filename:   filepath.Base(path),
		MIMEType:   mimeType,
		DataBase64: fmt.Sprintf("data:%s;base64,%s", mimeType, b64),
		SizeBytes:  int64(len(data)),
	}, nil
}

// BuildMessage constructs a multimodal schema.Message containing text and image attachments.
func BuildMessage(text string, attachments []*Attachment) (*schema.Message, error) {
	text = strings.TrimSpace(text)
	if len(attachments) == 0 {
		return schema.UserMessage(text), nil
	}

	parts := make([]schema.MessageInputPart, 0, len(attachments)+1)
	if text != "" {
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: text,
		})
	}

	for idx, att := range attachments {
		if att == nil {
			continue
		}
		part, err := attachmentToInputPart(att)
		if err != nil {
			return nil, fmt.Errorf("attachment %d: %w", idx+1, err)
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("empty multimodal message")
	}

	return &schema.Message{
		Role:                  schema.User,
		UserInputMultiContent: parts,
	}, nil
}

// AnalyzeImage calls the Domour LLM engine to perform vision inference on local image files.
func AnalyzeImage(ctx context.Context, cfg *LLMConfig, prompt string, imagePaths ...string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("llm config is required")
	}

	attachments := make([]*Attachment, 0, len(imagePaths))
	for _, p := range imagePaths {
		att, err := NewImageAttachmentFromFile(p)
		if err != nil {
			return "", fmt.Errorf("failed to attach %s: %w", p, err)
		}
		attachments = append(attachments, att)
	}

	msg, err := BuildMessage(prompt, attachments)
	if err != nil {
		return "", fmt.Errorf("failed to build multimodal message: %w", err)
	}

	chatModel, err := llm.NewChatModel(ctx, &llm.Config{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		BaseURL:  cfg.BaseURL,
		Model:    cfg.Model,
		ProxyURL: cfg.ProxyURL,
	})
	if err != nil {
		return "", fmt.Errorf("failed to initialize domour llm model (%s/%s): %w", cfg.Provider, cfg.Model, err)
	}

	res, err := chatModel.Generate(ctx, []*schema.Message{msg})
	if err != nil {
		return "", fmt.Errorf("llm vision inference failed: %w", err)
	}

	return res.Content, nil
}

func attachmentToInputPart(att *Attachment) (schema.MessageInputPart, error) {
	mimeType := strings.TrimSpace(att.MIMEType)
	if mimeType == "" && att.Filename != "" {
		mimeType = mime.TypeByExtension(filepath.Ext(att.Filename))
	}
	if mimeType == "" {
		mimeType = "image/jpeg"
	}

	base64Data := strings.TrimSpace(att.DataBase64)
	rawURL := strings.TrimSpace(att.URL)

	if base64Data == "" && rawURL == "" {
		return schema.MessageInputPart{}, fmt.Errorf("attachment has no URL or DataBase64")
	}

	partImage := &schema.MessageInputImage{
		MessagePartCommon: schema.MessagePartCommon{
			MIMEType: mimeType,
		},
		Detail: schema.ImageURLDetailHigh,
	}

	if base64Data != "" {
		partImage.Base64Data = &base64Data
	} else {
		partImage.URL = &rawURL
	}

	return schema.MessageInputPart{
		Type:  schema.ChatMessagePartTypeImageURL,
		Image: partImage,
	}, nil
}
