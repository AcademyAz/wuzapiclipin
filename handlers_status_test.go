package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
)

func testStatusPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatalf("could not encode test image: %v", err)
	}
	return output.Bytes()
}

func TestLoadStatusImageDataURL(t *testing.T) {
	pngData := testStatusPNG(t)
	source := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)

	data, mimeType, err := loadStatusImage(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(data, pngData) {
		t.Fatal("decoded image does not match the payload")
	}
	if mimeType != "image/png" {
		t.Fatalf("expected image/png, got %q", mimeType)
	}
}

func TestLoadStatusImageRejectsInvalidSources(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "empty", source: ""},
		{name: "local path", source: "/tmp/status.png"},
		{name: "not an image", source: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not-an-image"))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := loadStatusImage(context.Background(), test.source); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestValidateStatusMimeType(t *testing.T) {
	if err := validateStatusMimeType("image/png", "image/png"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateStatusMimeType("image/jpeg", "image/jpg"); err != nil {
		t.Fatalf("unexpected jpeg alias error: %v", err)
	}
	if err := validateStatusMimeType("image/png", "text/plain"); err == nil {
		t.Fatal("expected non-image MIME type to fail")
	}
	if err := validateStatusMimeType("image/png", "image/jpeg"); err == nil {
		t.Fatal("expected mismatched MIME type to fail")
	}
}

func TestBuildStatusImageMessage(t *testing.T) {
	pngData := testStatusPNG(t)
	uploaded := whatsmeow.UploadResponse{
		URL:           "https://mmg.whatsapp.net/test",
		DirectPath:    "/v/t62/test",
		MediaKey:      []byte("media-key"),
		FileEncSHA256: []byte("enc-sha"),
		FileSHA256:    []byte("sha"),
	}

	message, err := buildStatusImageMessage(pngData, "image/png", "  Agende agora  ", uploaded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	imageMessage := message.GetImageMessage()
	if imageMessage == nil {
		t.Fatal("expected an image message")
	}
	if imageMessage.GetCaption() != "Agende agora" {
		t.Fatalf("unexpected caption: %q", imageMessage.GetCaption())
	}
	if imageMessage.GetMimetype() != "image/png" {
		t.Fatalf("unexpected mimetype: %q", imageMessage.GetMimetype())
	}
	if imageMessage.GetURL() != uploaded.URL || imageMessage.GetDirectPath() != uploaded.DirectPath {
		t.Fatal("uploaded media metadata was not preserved")
	}
	if imageMessage.GetFileLength() != uint64(len(pngData)) {
		t.Fatalf("unexpected file length: %d", imageMessage.GetFileLength())
	}
	if len(imageMessage.GetJPEGThumbnail()) == 0 {
		t.Fatal("expected a generated JPEG thumbnail")
	}
}

func TestStatusSendRequestAcceptsUAZAPIContract(t *testing.T) {
	payload := `{"type":"image","file":"data:image/png;base64,AAAA","text":"Oferta do dia"}`

	var request statusSendRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if request.Type != "image" || !strings.HasPrefix(request.File, "data:image/png") || request.Text != "Oferta do dia" {
		t.Fatalf("unexpected request: %#v", request)
	}
}
