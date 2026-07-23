package multimodal_test

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/qtopie/domour/ark/multimodal"
)

func TestMultimodalAttachmentAndMessageBuilding(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "domour_multimodal_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	imgPath := filepath.Join(tmpDir, "test.jpg")
	createTestJPEG(t, imgPath, 100, 100)

	att, err := multimodal.NewImageAttachmentFromFile(imgPath)
	if err != nil {
		t.Fatalf("NewImageAttachmentFromFile failed: %v", err)
	}

	if att.Filename != "test.jpg" {
		t.Errorf("expected filename test.jpg, got %s", att.Filename)
	}
	if att.MIMEType != "image/jpeg" {
		t.Errorf("expected MIMEType image/jpeg, got %s", att.MIMEType)
	}

	msg, err := multimodal.BuildMessage("Analyze photo", []*multimodal.Attachment{att})
	if err != nil {
		t.Fatalf("BuildMessage failed: %v", err)
	}

	if msg.Role != schema.User {
		t.Errorf("expected Role User, got %s", msg.Role)
	}
	if len(msg.UserInputMultiContent) != 2 {
		t.Fatalf("expected 2 parts in UserInputMultiContent, got %d", len(msg.UserInputMultiContent))
	}
	if msg.UserInputMultiContent[0].Text != "Analyze photo" {
		t.Errorf("expected text 'Analyze photo', got %s", msg.UserInputMultiContent[0].Text)
	}
	if msg.UserInputMultiContent[1].Type != schema.ChatMessagePartTypeImageURL {
		t.Errorf("expected ImageURL part, got %s", msg.UserInputMultiContent[1].Type)
	}
}

func createTestJPEG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create image file: %v", err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("failed to encode jpeg: %v", err)
	}
}
