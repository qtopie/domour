package cognitor

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// Attachment defines a multimodal file or image attachment for Cognitor requests.
type Attachment struct {
	ID         string         `json:"id,omitempty"`
	Filename   string         `json:"filename,omitempty"`
	MIMEType   string         `json:"mime_type,omitempty"`
	URL        string         `json:"url,omitempty"`
	DataBase64 string         `json:"data_base64,omitempty"`
	SizeBytes  int64          `json:"size_bytes,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
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
	absPath, _ := filepath.Abs(path)
	return &Attachment{
		Filename:   filepath.Base(path),
		MIMEType:   mimeType,
		URL:        "file://" + absPath,
		DataBase64: fmt.Sprintf("data:%s;base64,%s", mimeType, b64),
		SizeBytes:  int64(len(data)),
	}, nil
}

// BuildMultimodalMessage constructs a schema.Message containing text and image attachments.
func BuildMultimodalMessage(text string, attachments []*Attachment) (*schema.Message, error) {
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

// LLMConfig is an alias for Config for backward compatibility with multimodal requests.
type LLMConfig = Config

// AnalyzeImage performs vision inference on local image files using Cognitor.
func AnalyzeImage(ctx context.Context, cfg *Config, prompt string, imagePaths ...string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config cannot be nil")
	}
	client, err := NewClient(ctx, *cfg)
	if err != nil {
		return "", fmt.Errorf("create cognitor client: %w", err)
	}
	return client.AnalyzeImage(ctx, prompt, imagePaths...)
}

func (c *Client) AnalyzeImage(ctx context.Context, prompt string, imagePaths ...string) (string, error) {
	attachments := make([]*Attachment, 0, len(imagePaths))
	for _, p := range imagePaths {
		att, err := NewImageAttachmentFromFile(p)
		if err != nil {
			return "", fmt.Errorf("failed to attach %s: %w", p, err)
		}
		attachments = append(attachments, att)
	}

	req := &Request{
		Message:     prompt,
		Attachments: attachments,
	}

	resp, err := c.Generate(ctx, req)
	if err != nil {
		return "", fmt.Errorf("cognitor image analysis failed: %w", err)
	}

	return resp.Content, nil
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
	}
	if rawURL != "" {
		partImage.URL = &rawURL
	}

	return schema.MessageInputPart{
		Type:  schema.ChatMessagePartTypeImageURL,
		Image: partImage,
	}, nil
}
