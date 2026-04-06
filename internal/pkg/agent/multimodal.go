package agent

import (
	"fmt"
	"mime"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
	commonpb "github.com/qtopie/domour/gen/assistant/common"
)

func attachmentsFromProto(items []*commonpb.Attachment) []BrainAttachment {
	if len(items) == 0 {
		return nil
	}

	attachments := make([]BrainAttachment, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		attachment := BrainAttachment{
			ID:         strings.TrimSpace(item.GetId()),
			Filename:   strings.TrimSpace(item.GetFilename()),
			MIMEType:   strings.TrimSpace(item.GetMimeType()),
			URL:        strings.TrimSpace(item.GetUrl()),
			DataBase64: strings.TrimSpace(item.GetDataBase64()),
			SizeBytes:  item.GetSizeBytes(),
		}
		if metadata := item.GetMetadata(); metadata != nil {
			attachment.Metadata = metadata.AsMap()
		}
		attachments = append(attachments, attachment)
	}
	if len(attachments) == 0 {
		return nil
	}
	return attachments
}

func buildUserInputMessage(text string, attachments []BrainAttachment) (*schema.Message, error) {
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

	for idx, attachment := range attachments {
		part, err := attachmentToInputPart(attachment)
		if err != nil {
			return nil, fmt.Errorf("attachment %d: %w", idx+1, err)
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("multimodal request is empty")
	}

	return &schema.Message{
		Role:                  schema.User,
		UserInputMultiContent: parts,
	}, nil
}

func attachmentToInputPart(attachment BrainAttachment) (schema.MessageInputPart, error) {
	mimeType := normalizeAttachmentMIMEType(attachment)
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		image, err := attachmentToInputImage(attachment, mimeType)
		if err != nil {
			return schema.MessageInputPart{}, err
		}
		return schema.MessageInputPart{
			Type:  schema.ChatMessagePartTypeImageURL,
			Image: image,
		}, nil
	case strings.HasPrefix(mimeType, "audio/"):
		return schema.MessageInputPart{}, fmt.Errorf("audio attachments are not supported yet")
	case strings.HasPrefix(mimeType, "video/"):
		return schema.MessageInputPart{}, fmt.Errorf("video attachments are not supported yet")
	case mimeType == "":
		return schema.MessageInputPart{}, fmt.Errorf("attachment %q is missing a supported mime type", attachmentName(attachment))
	default:
		return schema.MessageInputPart{}, fmt.Errorf("attachment %q with mime type %q is not supported", attachmentName(attachment), mimeType)
	}
}

func attachmentToInputImage(attachment BrainAttachment, mimeType string) (*schema.MessageInputImage, error) {
	base64Data := strings.TrimSpace(attachment.DataBase64)
	rawURL := strings.TrimSpace(attachment.URL)
	if base64Data == "" && rawURL == "" {
		return nil, fmt.Errorf("image attachment %q must include url or data_base64", attachmentName(attachment))
	}

	part := &schema.MessageInputImage{
		MessagePartCommon: schema.MessagePartCommon{
			MIMEType: mimeType,
		},
		Detail: schema.ImageURLDetailHigh,
	}
	if base64Data != "" {
		part.Base64Data = &base64Data
		return part, nil
	}
	part.URL = &rawURL
	return part, nil
}

func normalizeAttachmentMIMEType(attachment BrainAttachment) string {
	if mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType)); mimeType != "" {
		return mimeType
	}

	ext := filepath.Ext(strings.TrimSpace(attachment.Filename))
	if ext == "" {
		if rawURL := strings.TrimSpace(attachment.URL); rawURL != "" {
			if parsed, err := url.Parse(rawURL); err == nil {
				ext = path.Ext(parsed.Path)
			}
		}
	}
	if ext == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(mime.TypeByExtension(ext)))
}

func attachmentName(attachment BrainAttachment) string {
	for _, value := range []string{
		strings.TrimSpace(attachment.Filename),
		strings.TrimSpace(attachment.ID),
		strings.TrimSpace(attachment.URL),
	} {
		if value != "" {
			return value
		}
	}
	return "attachment"
}
