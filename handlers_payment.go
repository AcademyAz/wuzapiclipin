package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/vincent-petithory/dataurl"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

const paymentAmountOffset int64 = 100

var (
	paymentReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,60}$`)
	nonDigitPattern         = regexp.MustCompile(`\D`)
)

type requestPaymentPayload struct {
	Number        string            `json:"number"`
	Phone         string            `json:"Phone"`
	Title         string            `json:"title"`
	Text          string            `json:"text"`
	Footer        string            `json:"footer"`
	ItemName      string            `json:"itemName"`
	InvoiceNumber string            `json:"invoiceNumber"`
	Amount        float64           `json:"amount"`
	Currency      string            `json:"currency"`
	PixKey        string            `json:"pixKey"`
	PixCode       string            `json:"pixCode"`
	PixType       string            `json:"pixType"`
	PixName       string            `json:"pixName"`
	PaymentLink   string            `json:"paymentLink"`
	FileURL       string            `json:"fileUrl"`
	FileName      string            `json:"fileName"`
	BoletoCode    string            `json:"boletoCode"`
	ID            string            `json:"id"`
	ContextInfo   waE2E.ContextInfo `json:"ContextInfo"`
	QuotedMessage *waE2E.Message    `json:"QuotedMessage,omitempty"`
}

type paymentAmount struct {
	Value  int64 `json:"value"`
	Offset int64 `json:"offset"`
}

type paymentItem struct {
	RetailerID    string        `json:"retailer_id"`
	Name          string        `json:"name"`
	Amount        paymentAmount `json:"amount"`
	Quantity      int           `json:"quantity"`
	IsCustomItem  bool          `json:"isCustomItem,omitempty"`
	IsQuantitySet bool          `json:"isQuantitySet,omitempty"`
}

type paymentOrder struct {
	Status   string        `json:"status"`
	Items    []paymentItem `json:"items"`
	Subtotal paymentAmount `json:"subtotal"`
}

type pixPaymentSetting struct {
	MerchantName string `json:"merchant_name"`
	Key          string `json:"key"`
	KeyType      string `json:"key_type"`
	Code         string `json:"code,omitempty"`
}

type boletoPaymentSetting struct {
	DigitableLine string `json:"digitable_line"`
}

type linkPaymentSetting struct {
	URI string `json:"uri"`
}

type paymentSetting struct {
	Type           string                `json:"type"`
	PixStaticCode  *pixPaymentSetting    `json:"pix_static_code,omitempty"`
	PixDynamicCode *pixPaymentSetting    `json:"pix_dynamic_code,omitempty"`
	Boleto         *boletoPaymentSetting `json:"boleto,omitempty"`
	PaymentLink    *linkPaymentSetting   `json:"payment_link,omitempty"`
}

type requestPaymentParams struct {
	ReferenceID        string           `json:"reference_id"`
	Type               string           `json:"type"`
	PaymentType        string           `json:"payment_type"`
	PaymentSettings    []paymentSetting `json:"payment_settings"`
	Currency           string           `json:"currency"`
	TotalAmount        paymentAmount    `json:"total_amount"`
	Order              paymentOrder     `json:"order"`
	SharePaymentStatus bool             `json:"share_payment_status"`
}

func normalizePaymentPayload(payload *requestPaymentPayload, messageID string) error {
	payload.Number = strings.TrimSpace(payload.Number)
	if payload.Number == "" {
		payload.Number = strings.TrimSpace(payload.Phone)
	}
	if payload.Number == "" {
		return errors.New("number is required")
	}

	payload.ItemName = strings.TrimSpace(payload.ItemName)
	if payload.ItemName == "" {
		return errors.New("itemName is required")
	}
	if len([]rune(payload.ItemName)) > 60 {
		return errors.New("itemName must contain at most 60 characters")
	}
	if math.IsNaN(payload.Amount) || math.IsInf(payload.Amount, 0) || payload.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	if math.Round(payload.Amount*100) > math.MaxInt64 {
		return errors.New("amount is too large")
	}

	payload.Currency = strings.ToUpper(strings.TrimSpace(payload.Currency))
	if payload.Currency == "" {
		payload.Currency = "BRL"
	}
	if payload.Currency != "BRL" {
		return errors.New("currency must be BRL")
	}

	payload.InvoiceNumber = strings.TrimSpace(payload.InvoiceNumber)
	if payload.InvoiceNumber == "" {
		payload.InvoiceNumber = messageID
	}
	if !paymentReferencePattern.MatchString(payload.InvoiceNumber) {
		return errors.New("invoiceNumber must contain 1 to 60 letters, numbers, underscores, hyphens or dots")
	}

	payload.PixKey = strings.TrimSpace(payload.PixKey)
	payload.PixCode = strings.TrimSpace(payload.PixCode)
	payload.PixName = strings.TrimSpace(payload.PixName)
	payload.PixType = strings.ToUpper(strings.TrimSpace(payload.PixType))
	if payload.PixKey != "" || payload.PixCode != "" {
		if payload.PixKey == "" {
			return errors.New("pixKey is required when pixCode is provided")
		}
		if payload.PixName == "" {
			return errors.New("pixName is required when PIX is enabled")
		}
		if payload.PixType == "" {
			payload.PixType = "EVP"
		}
		switch payload.PixType {
		case "CPF", "CNPJ", "PHONE", "EMAIL", "EVP":
		default:
			return errors.New("pixType must be CPF, CNPJ, PHONE, EMAIL or EVP")
		}
	}

	payload.BoletoCode = nonDigitPattern.ReplaceAllString(payload.BoletoCode, "")
	if payload.BoletoCode != "" && (len(payload.BoletoCode) < 44 || len(payload.BoletoCode) > 48) {
		return errors.New("boletoCode must contain between 44 and 48 digits")
	}

	payload.PaymentLink = strings.TrimSpace(payload.PaymentLink)
	if payload.PaymentLink != "" {
		parsed, err := url.ParseRequestURI(payload.PaymentLink)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return errors.New("paymentLink must be a valid HTTPS URL")
		}
	}

	if payload.PixKey == "" && payload.BoletoCode == "" && payload.PaymentLink == "" {
		return errors.New("at least one payment method is required")
	}

	payload.FileURL = strings.TrimSpace(payload.FileURL)
	payload.FileName = strings.TrimSpace(payload.FileName)
	if payload.FileURL != "" && payload.FileName == "" {
		payload.FileName = "cobranca.pdf"
	}
	return nil
}

func buildRequestPaymentParams(payload requestPaymentPayload) requestPaymentParams {
	amount := paymentAmount{Value: int64(math.Round(payload.Amount * 100)), Offset: paymentAmountOffset}
	settings := make([]paymentSetting, 0, 3)

	if payload.PixKey != "" {
		pix := &pixPaymentSetting{
			MerchantName: payload.PixName,
			Key:          payload.PixKey,
			KeyType:      payload.PixType,
		}
		setting := paymentSetting{Type: "pix_static_code", PixStaticCode: pix}
		if payload.PixCode != "" {
			pix.Code = payload.PixCode
			setting = paymentSetting{Type: "pix_dynamic_code", PixDynamicCode: pix}
		}
		settings = append(settings, setting)
	}
	if payload.BoletoCode != "" {
		settings = append(settings, paymentSetting{
			Type:   "boleto",
			Boleto: &boletoPaymentSetting{DigitableLine: payload.BoletoCode},
		})
	}
	if payload.PaymentLink != "" {
		settings = append(settings, paymentSetting{
			Type:        "payment_link",
			PaymentLink: &linkPaymentSetting{URI: payload.PaymentLink},
		})
	}

	item := paymentItem{
		RetailerID:    payload.InvoiceNumber,
		Name:          payload.ItemName,
		Amount:        amount,
		Quantity:      1,
		IsCustomItem:  true,
		IsQuantitySet: true,
	}
	return requestPaymentParams{
		ReferenceID:     payload.InvoiceNumber,
		Type:            "digital-goods",
		PaymentType:     "br",
		PaymentSettings: settings,
		Currency:        payload.Currency,
		TotalAmount:     amount,
		Order: paymentOrder{
			Status:   "pending",
			Items:    []paymentItem{item},
			Subtotal: amount,
		},
		SharePaymentStatus: false,
	}
}

func buildPaymentDocument(ctx context.Context, client *whatsmeow.Client, payload requestPaymentPayload) (*waE2E.DocumentMessage, error) {
	if payload.FileURL == "" {
		return nil, nil
	}

	var fileData []byte
	mimeType := ""
	if strings.HasPrefix(payload.FileURL, "data:") {
		decoded, err := dataurl.DecodeString(payload.FileURL)
		if err != nil {
			return nil, errors.New("could not decode fileUrl data URL")
		}
		fileData = decoded.Data
		mimeType = decoded.MediaType.ContentType()
	} else if isHTTPURL(payload.FileURL) {
		data, contentType, err := fetchURLBytes(ctx, payload.FileURL, fetchDocumentMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch fileUrl: %w", err)
		}
		fileData = data
		mimeType = contentType
	} else {
		return nil, errors.New("fileUrl must be a data URL or a valid HTTP URL")
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(fileData)
	}

	uploaded, err := client.Upload(ctx, fileData, whatsmeow.MediaDocument)
	if err != nil {
		return nil, fmt.Errorf("failed to upload payment document: %w", err)
	}
	return &waE2E.DocumentMessage{
		URL:           proto.String(uploaded.URL),
		FileName:      proto.String(payload.FileName),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(fileData))),
	}, nil
}

// SendRequestPayment sends a native WhatsApp order_details message with one or
// more Brazilian payment methods (PIX, boleto and/or an approved payment link).
func (s *server) SendRequestPayment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value("userinfo").(Values).Get("Id")
		client := clientManager.GetWhatsmeowClient(userID)
		if client == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("no session"))
			return
		}

		var payload requestPaymentPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("could not decode Payload"))
			return
		}

		messageID := strings.TrimSpace(payload.ID)
		if messageID == "" {
			messageID = client.GenerateMessageID()
		}
		if err := normalizePaymentPayload(&payload, messageID); err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}

		recipient, err := validateMessageFields(r.Context(), client, payload.Number, payload.ContextInfo.StanzaID, payload.ContextInfo.Participant)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}

		paramsJSON, err := json.Marshal(buildRequestPaymentParams(payload))
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("could not encode payment parameters: %w", err))
			return
		}

		var contextInfo waE2E.ContextInfo
		if payload.ContextInfo.StanzaID != nil {
			quoted := payload.QuotedMessage
			if quoted == nil {
				quoted = &waE2E.Message{Conversation: proto.String("")}
			}
			contextInfo.StanzaID = payload.ContextInfo.StanzaID
			contextInfo.Participant = payload.ContextInfo.Participant
			contextInfo.QuotedMessage = quoted
		}
		contextInfo.MentionedJID = payload.ContextInfo.MentionedJID
		contextInfo.IsForwarded = payload.ContextInfo.IsForwarded

		header := &waE2E.InteractiveMessage_Header{}
		if payload.Title != "" {
			header.Title = proto.String(payload.Title)
		}
		document, err := buildPaymentDocument(r.Context(), client, payload)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, err)
			return
		}
		if document != nil {
			header.HasMediaAttachment = proto.Bool(true)
			header.Media = &waE2E.InteractiveMessage_Header_DocumentMessage{DocumentMessage: document}
		}

		body := strings.TrimSpace(payload.Text)
		if body == "" {
			body = "Revise os detalhes da cobrança e escolha uma forma de pagamento."
		}
		messageParamsJSON := `{"bottom_sheet":{"in_thread_buttons_limit":5,"divider_indices":[]}}`
		interactive := &waE2E.InteractiveMessage{
			Header:      header,
			Body:        &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
			ContextInfo: &contextInfo,
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
						Name:             proto.String("review_and_pay"),
						ButtonParamsJSON: proto.String(string(paramsJSON)),
					}},
					MessageParamsJSON: proto.String(messageParamsJSON),
					MessageVersion:    proto.Int32(1),
				},
			},
		}
		if payload.Footer != "" {
			interactive.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(payload.Footer)}
		}

		messageSecret := make([]byte, 32)
		if _, err := cryptorand.Read(messageSecret); err != nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("could not generate message secret"))
			return
		}
		message := &waE2E.Message{
			InteractiveMessage: interactive,
			MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: messageSecret},
		}
		extraNodes := []waBinary.Node{{
			Tag:   "biz",
			Attrs: waBinary.Attrs{"native_flow_name": "order_details"},
		}}

		response, err := client.SendMessage(r.Context(), recipient, message, whatsmeow.SendRequestExtra{
			ID:              messageID,
			AdditionalNodes: &extraNodes,
		})
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("error sending payment request: %w", err))
			return
		}

		historyLimit, _ := strconv.Atoi(r.Context().Value("userinfo").(Values).Get("History"))
		s.saveOutgoingMessageToHistory(userID, recipient.String(), messageID, "request-payment", body, "", historyLimit)
		token := r.Context().Value("userinfo").(Values).Get("Token")
		s.publishSentMessageEvent(token, userID, userID, recipient, messageID, message, response.Timestamp, "request-payment")

		responseJSON, _ := json.Marshal(map[string]interface{}{
			"Details":   "Sent",
			"Timestamp": response.Timestamp.Unix(),
			"Id":        messageID,
		})
		s.Respond(w, r, http.StatusOK, string(responseJSON))
	}
}
