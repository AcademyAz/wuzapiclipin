package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/vincent-petithory/dataurl"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const (
	minCarouselCards      = 2
	maxCarouselCards      = 10
	maxCarouselButtons    = 2
	maxCarouselButtonText = 20
)

type carouselButtonRequest struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	ButtonText  string `json:"buttonText"`
	ID          string `json:"id"`
	ButtonID    string `json:"buttonId"`
	URL         string `json:"url"`
	PhoneNumber string `json:"phone_number"`
	CopyCode    string `json:"copy_code"`
}

type carouselCardRequest struct {
	Body    string                  `json:"Body"`
	Text    string                  `json:"text"`
	Title   string                  `json:"title"`
	Footer  string                  `json:"Footer"`
	Image   string                  `json:"Image"`
	Image2  string                  `json:"image"`
	Buttons []carouselButtonRequest `json:"Buttons"`
}

type carouselRequest struct {
	Phone    string                `json:"Phone"`
	Number   string                `json:"number"`
	Body     string                `json:"Body"`
	Text     string                `json:"text"`
	Footer   string                `json:"Footer"`
	Cards    []carouselCardRequest `json:"Cards"`
	Carousel []carouselCardRequest `json:"carousel"`
	ID       string                `json:"Id"`

	ContextInfo   waE2E.ContextInfo `json:"ContextInfo"`
	QuotedMessage *waE2E.Message    `json:"QuotedMessage,omitempty"`
}

func firstCarouselValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateCarouselText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return string(runes)
}

func buildCarouselButtons(requests []carouselButtonRequest) ([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, error) {
	if len(requests) == 0 {
		return nil, errors.New("at least one button is required per card")
	}
	if len(requests) > maxCarouselButtons {
		return nil, fmt.Errorf("a carousel card supports at most %d buttons", maxCarouselButtons)
	}

	buttons := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, 0, len(requests))
	for index, request := range requests {
		title := truncateCarouselText(firstCarouselValue(request.Title, request.Text, request.ButtonText), maxCarouselButtonText)
		if title == "" {
			return nil, fmt.Errorf("button %d is missing title/text", index+1)
		}

		buttonType := strings.ToLower(firstCarouselValue(request.Type, "reply"))
		var name string
		var params map[string]string

		switch buttonType {
		case "reply", "quick_reply":
			id := firstCarouselValue(request.ID, request.ButtonID, title)
			name = "quick_reply"
			params = map[string]string{"display_text": title, "id": id}

		case "url", "cta_url":
			target := firstCarouselValue(request.URL, request.ID, request.ButtonID)
			parsed, err := url.ParseRequestURI(target)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return nil, fmt.Errorf("button %d has an invalid URL", index+1)
			}
			name = "cta_url"
			params = map[string]string{"display_text": title, "url": target, "merchant_url": target}

		case "call", "cta_call":
			phone := firstCarouselValue(request.PhoneNumber, request.ID, request.ButtonID)
			if phone == "" {
				return nil, fmt.Errorf("button %d is missing phone number", index+1)
			}
			name = "cta_call"
			params = map[string]string{"display_text": title, "phone_number": phone}

		case "copy", "cta_copy":
			code := firstCarouselValue(request.CopyCode, request.ID, request.ButtonID)
			if code == "" {
				return nil, fmt.Errorf("button %d is missing copy code", index+1)
			}
			name = "cta_copy"
			params = map[string]string{"display_text": title, "copy_code": code}

		default:
			return nil, fmt.Errorf("button %d has unsupported type %q", index+1, request.Type)
		}

		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("button %d params: %w", index+1, err)
		}
		buttons = append(buttons, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			Name:             proto.String(name),
			ButtonParamsJSON: proto.String(string(paramsJSON)),
		})
	}

	return buttons, nil
}

func loadCarouselImage(ctx context.Context, source string) ([]byte, string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, "", errors.New("image is required")
	}

	var data []byte
	var err error
	if strings.HasPrefix(source, "data:image/") {
		decoded, decodeErr := dataurl.DecodeString(source)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("invalid image data URL: %w", decodeErr)
		}
		data = decoded.Data
	} else if isHTTPURL(source) {
		data, _, err = fetchURLBytes(ctx, source, fetchImageMaxBytes)
		if err != nil {
			return nil, "", fmt.Errorf("could not download image: %w", err)
		}
	} else {
		return nil, "", errors.New("image must be an HTTP(S) URL or data:image URL")
	}

	if len(data) == 0 {
		return nil, "", errors.New("image is empty")
	}

	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", fmt.Errorf("unsupported image content type %q", mimeType)
	}
	return data, mimeType, nil
}

func carouselContextInfo(request *carouselRequest) *waE2E.ContextInfo {
	contextInfo := &waE2E.ContextInfo{}
	if request.ContextInfo.StanzaID != nil {
		contextInfo.StanzaID = proto.String(request.ContextInfo.GetStanzaID())
		if request.ContextInfo.Participant != nil {
			contextInfo.Participant = proto.String(request.ContextInfo.GetParticipant())
		}
		quoted := request.QuotedMessage
		if quoted == nil {
			quoted = &waE2E.Message{Conversation: proto.String("")}
		}
		contextInfo.QuotedMessage = quoted
	}
	contextInfo.MentionedJID = request.ContextInfo.MentionedJID
	contextInfo.IsForwarded = request.ContextInfo.IsForwarded
	return contextInfo
}

// SendCarousel sends 2-10 horizontally scrollable image cards with NativeFlow buttons.
func (s *server) SendCarousel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		txtid := r.Context().Value("userinfo").(Values).Get("Id")
		client := clientManager.GetWhatsmeowClient(txtid)
		if client == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("no session"))
			return
		}

		var request carouselRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("could not decode Payload"))
			return
		}

		phone := firstCarouselValue(request.Phone, request.Number)
		body := firstCarouselValue(request.Body, request.Text, "Confira nossas opções:")
		cardsRequest := request.Cards
		if len(cardsRequest) == 0 {
			cardsRequest = request.Carousel
		}

		if phone == "" {
			s.Respond(w, r, http.StatusBadRequest, errors.New("missing Phone in Payload"))
			return
		}
		if len(cardsRequest) < minCarouselCards || len(cardsRequest) > maxCarouselCards {
			s.Respond(w, r, http.StatusBadRequest, fmt.Errorf("carousel requires between %d and %d cards", minCarouselCards, maxCarouselCards))
			return
		}

		recipient, err := validateMessageFields(r.Context(), client, phone, request.ContextInfo.StanzaID, request.ContextInfo.Participant)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}

		cards := make([]*waE2E.InteractiveMessage, 0, len(cardsRequest))
		for index, cardRequest := range cardsRequest {
			cardBody := firstCarouselValue(cardRequest.Body, cardRequest.Text, cardRequest.Title)
			if cardBody == "" {
				s.Respond(w, r, http.StatusBadRequest, fmt.Errorf("card %d is missing Body/text", index+1))
				return
			}

			buttons, err := buildCarouselButtons(cardRequest.Buttons)
			if err != nil {
				s.Respond(w, r, http.StatusBadRequest, fmt.Errorf("card %d: %w", index+1, err))
				return
			}

			imageSource := firstCarouselValue(cardRequest.Image, cardRequest.Image2)
			imageData, mimeType, err := loadCarouselImage(r.Context(), imageSource)
			if err != nil {
				s.Respond(w, r, http.StatusBadRequest, fmt.Errorf("card %d: %w", index+1, err))
				return
			}

			uploaded, err := client.Upload(r.Context(), imageData, whatsmeow.MediaImage)
			if err != nil {
				s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("card %d image upload: %w", index+1, err))
				return
			}

			card := &waE2E.InteractiveMessage{
				Header: &waE2E.InteractiveMessage_Header{
					HasMediaAttachment: proto.Bool(true),
					Media: &waE2E.InteractiveMessage_Header_ImageMessage{
						ImageMessage: &waE2E.ImageMessage{
							URL:           proto.String(uploaded.URL),
							DirectPath:    proto.String(uploaded.DirectPath),
							MediaKey:      uploaded.MediaKey,
							Mimetype:      proto.String(mimeType),
							FileEncSHA256: uploaded.FileEncSHA256,
							FileSHA256:    uploaded.FileSHA256,
							FileLength:    proto.Uint64(uint64(len(imageData))),
						},
					},
				},
				Body: &waE2E.InteractiveMessage_Body{Text: proto.String(cardBody)},
				InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
					NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
						Buttons:           buttons,
						MessageParamsJSON: proto.String(fmt.Sprintf(`{"from":"api","templateId":"%s-%d"}`, client.GenerateMessageID(), index+1)),
						MessageVersion:    proto.Int32(1),
					},
				},
			}
			if footer := strings.TrimSpace(cardRequest.Footer); footer != "" {
				card.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
			}
			cards = append(cards, card)
		}

		cardType := waE2E.InteractiveMessage_CarouselMessage_HSCROLL_CARDS
		interactiveMessage := &waE2E.InteractiveMessage{
			Body:        &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
			ContextInfo: carouselContextInfo(&request),
			InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{
				CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
					Cards:            cards,
					MessageVersion:   proto.Int32(1),
					CarouselCardType: &cardType,
				},
			},
		}
		if footer := strings.TrimSpace(request.Footer); footer != "" {
			interactiveMessage.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}
		}

		finalMessage := &waE2E.Message{
			InteractiveMessage: interactiveMessage,
			MessageContextInfo: &waE2E.MessageContextInfo{
				DeviceListMetadata: &waE2E.DeviceListMetadata{},
			},
		}

		extraNodes := []waBinary.Node{{
			Tag: "biz",
			Content: []waBinary.Node{{
				Tag:   "interactive",
				Attrs: waBinary.Attrs{"type": "native_flow", "v": "1"},
				Content: []waBinary.Node{{
					Tag:   "native_flow",
					Attrs: waBinary.Attrs{"v": "9", "name": "mixed"},
				}},
			}},
		}}

		messageID := strings.TrimSpace(request.ID)
		if messageID == "" {
			messageID = client.GenerateMessageID()
		}

		response, err := client.SendMessage(r.Context(), recipient, finalMessage, whatsmeow.SendRequestExtra{
			ID:              messageID,
			AdditionalNodes: &extraNodes,
		})
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("error sending carousel: %w", err))
			return
		}

		historyLimit, _ := strconv.Atoi(r.Context().Value("userinfo").(Values).Get("History"))
		s.saveOutgoingMessageToHistory(txtid, recipient.String(), messageID, "carousel", body, "", historyLimit)

		token := r.Context().Value("userinfo").(Values).Get("Token")
		s.publishSentMessageEvent(token, txtid, txtid, recipient, messageID, finalMessage, response.Timestamp)

		responseJSON, _ := json.Marshal(map[string]interface{}{
			"Details":   "Sent",
			"Timestamp": response.Timestamp.Unix(),
			"Id":        messageID,
		})
		s.Respond(w, r, http.StatusOK, string(responseJSON))
	}
}
