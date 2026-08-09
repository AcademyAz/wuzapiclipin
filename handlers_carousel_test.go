package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCarouselButtonsReply(t *testing.T) {
	buttons, err := buildCarouselButtons([]carouselButtonRequest{{
		Type: "REPLY",
		Text: "Agendar",
		ID:   "service-123",
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(buttons) != 1 || buttons[0].GetName() != "quick_reply" {
		t.Fatalf("unexpected buttons: %#v", buttons)
	}

	var params map[string]string
	if err := json.Unmarshal([]byte(buttons[0].GetButtonParamsJSON()), &params); err != nil {
		t.Fatalf("invalid params JSON: %v", err)
	}
	if params["display_text"] != "Agendar" || params["id"] != "service-123" {
		t.Fatalf("unexpected params: %#v", params)
	}
}

func TestBuildCarouselButtonsURLWithLegacyID(t *testing.T) {
	buttons, err := buildCarouselButtons([]carouselButtonRequest{{
		Type: "URL",
		Text: "Detalhes",
		ID:   "https://example.com/service",
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buttons[0].GetName() != "cta_url" {
		t.Fatalf("unexpected button name: %s", buttons[0].GetName())
	}
}

func TestBuildCarouselButtonsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		buttons []carouselButtonRequest
	}{
		{name: "empty", buttons: nil},
		{name: "too many", buttons: []carouselButtonRequest{{Text: "1"}, {Text: "2"}, {Text: "3"}}},
		{name: "invalid URL", buttons: []carouselButtonRequest{{Type: "URL", Text: "Abrir", ID: "javascript:alert(1)"}}},
		{name: "unknown type", buttons: []carouselButtonRequest{{Type: "OTHER", Text: "Abrir"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := buildCarouselButtons(test.buttons); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestBuildCarouselButtonsTruncatesUnicodeTitle(t *testing.T) {
	title := strings.Repeat("á", maxCarouselButtonText+5)
	buttons, err := buildCarouselButtons([]carouselButtonRequest{{Text: title}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var params map[string]string
	if err := json.Unmarshal([]byte(buttons[0].GetButtonParamsJSON()), &params); err != nil {
		t.Fatalf("invalid params JSON: %v", err)
	}
	if got := len([]rune(params["display_text"])); got != maxCarouselButtonText {
		t.Fatalf("expected %d runes, got %d", maxCarouselButtonText, got)
	}
}

func TestLoadCarouselImageDataURL(t *testing.T) {
	data, mimeType, err := loadCarouselImage(context.Background(), "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 || mimeType != "image/png" {
		t.Fatalf("unexpected image: bytes=%d mime=%s", len(data), mimeType)
	}
}

func TestCarouselRequestAcceptsUAZAPIPayload(t *testing.T) {
	payload := `{
		"number":"5562999999999",
		"text":"Produtos",
		"carousel":[
			{"text":"Produto 1","image":"data:image/png;base64,iVBORw0KGgo=","buttons":[{"id":"1","text":"Comprar","type":"REPLY"}]},
			{"text":"Produto 2","image":"data:image/png;base64,iVBORw0KGgo=","buttons":[{"id":"2","text":"Comprar","type":"REPLY"}]}
		]
	}`

	var request carouselRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if request.Number != "5562999999999" || len(request.Carousel) != 2 {
		t.Fatalf("unexpected request: number=%q cards=%d", request.Number, len(request.Carousel))
	}
	if request.Carousel[0].Image2 == "" || request.Carousel[0].Buttons[0].Text != "Comprar" {
		t.Fatalf("legacy fields were not decoded: %#v", request.Carousel[0])
	}
}
