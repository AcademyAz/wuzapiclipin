package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func validPaymentPayload() requestPaymentPayload {
	return requestPaymentPayload{
		Number:        "5511999999999",
		ItemName:      "Assinatura Clipin",
		InvoiceNumber: "pay_123.ABC",
		Amount:        109.90,
		PixKey:        "123e4567-e89b-12d3-a456-426614174000",
		PixType:       "evp",
		PixName:       "Clipin",
	}
}

func TestNormalizePaymentPayloadDefaultsAndStaticPIX(t *testing.T) {
	payload := validPaymentPayload()
	if err := normalizePaymentPayload(&payload, "GENERATED-ID"); err != nil {
		t.Fatalf("normalizePaymentPayload returned error: %v", err)
	}

	if payload.Currency != "BRL" {
		t.Fatalf("expected BRL, got %q", payload.Currency)
	}
	if payload.PixType != "EVP" {
		t.Fatalf("expected EVP, got %q", payload.PixType)
	}

	params := buildRequestPaymentParams(payload)
	if params.TotalAmount.Value != 10990 || params.TotalAmount.Offset != 100 {
		t.Fatalf("unexpected total amount: %+v", params.TotalAmount)
	}
	if params.ReferenceID != payload.InvoiceNumber {
		t.Fatalf("unexpected reference ID: %q", params.ReferenceID)
	}
	if len(params.PaymentSettings) != 1 || params.PaymentSettings[0].Type != "pix_static_code" {
		t.Fatalf("unexpected payment settings: %+v", params.PaymentSettings)
	}
	if params.PaymentSettings[0].PixStaticCode == nil || params.PaymentSettings[0].PixStaticCode.KeyType != "EVP" {
		t.Fatalf("unexpected PIX setting: %+v", params.PaymentSettings[0])
	}
	if len(params.Order.Items) != 1 || params.Order.Items[0].Quantity != 1 {
		t.Fatalf("unexpected items: %+v", params.Order.Items)
	}
	if params.Order.Subtotal != params.TotalAmount {
		t.Fatalf("subtotal and total differ: %+v / %+v", params.Order.Subtotal, params.TotalAmount)
	}
}

func TestBuildRequestPaymentParamsWithAllMethods(t *testing.T) {
	payload := validPaymentPayload()
	payload.PixCode = "00020101021226890014BR.GOV.BCB.PIX"
	payload.BoletoCode = "34191.79001 01043.510047 91020.150008 5 91070026000"
	payload.PaymentLink = "https://pay.example.com/checkout/pay_123"

	if err := normalizePaymentPayload(&payload, "GENERATED-ID"); err != nil {
		t.Fatalf("normalizePaymentPayload returned error: %v", err)
	}
	params := buildRequestPaymentParams(payload)
	if len(params.PaymentSettings) != 3 {
		t.Fatalf("expected 3 payment settings, got %+v", params.PaymentSettings)
	}
	if params.PaymentSettings[0].Type != "pix_dynamic_code" || params.PaymentSettings[0].PixDynamicCode == nil {
		t.Fatalf("unexpected dynamic PIX setting: %+v", params.PaymentSettings[0])
	}
	if params.PaymentSettings[0].PixDynamicCode.Code != payload.PixCode {
		t.Fatalf("unexpected dynamic PIX code")
	}
	if params.PaymentSettings[1].Boleto == nil || strings.ContainsAny(params.PaymentSettings[1].Boleto.DigitableLine, " .") {
		t.Fatalf("boleto code was not normalized: %+v", params.PaymentSettings[1])
	}
	if params.PaymentSettings[2].PaymentLink == nil || params.PaymentSettings[2].PaymentLink.URI != payload.PaymentLink {
		t.Fatalf("unexpected payment link setting: %+v", params.PaymentSettings[2])
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("could not marshal params: %v", err)
	}
	for _, expected := range []string{`"payment_type":"br"`, `"type":"boleto"`, `"type":"payment_link"`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("encoded params do not contain %s: %s", expected, encoded)
		}
	}
}

func TestNormalizePaymentPayloadUsesPhoneAndGeneratedReference(t *testing.T) {
	payload := validPaymentPayload()
	payload.Number = ""
	payload.Phone = "5511888888888"
	payload.InvoiceNumber = ""

	if err := normalizePaymentPayload(&payload, "GENERATED-ID"); err != nil {
		t.Fatalf("normalizePaymentPayload returned error: %v", err)
	}
	if payload.Number != payload.Phone {
		t.Fatalf("expected Phone alias to be used, got %q", payload.Number)
	}
	if payload.InvoiceNumber != "GENERATED-ID" {
		t.Fatalf("expected generated reference, got %q", payload.InvoiceNumber)
	}
}

func TestNormalizePaymentPayloadValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*requestPaymentPayload)
		wantErr string
	}{
		{name: "missing recipient", mutate: func(p *requestPaymentPayload) { p.Number = "" }, wantErr: "number is required"},
		{name: "missing item", mutate: func(p *requestPaymentPayload) { p.ItemName = "" }, wantErr: "itemName is required"},
		{name: "invalid amount", mutate: func(p *requestPaymentPayload) { p.Amount = 0 }, wantErr: "amount must be greater than zero"},
		{name: "invalid reference", mutate: func(p *requestPaymentPayload) { p.InvoiceNumber = "invoice with spaces" }, wantErr: "invoiceNumber"},
		{name: "invalid pix type", mutate: func(p *requestPaymentPayload) { p.PixType = "RANDOM" }, wantErr: "pixType"},
		{name: "missing pix name", mutate: func(p *requestPaymentPayload) { p.PixName = "" }, wantErr: "pixName"},
		{name: "invalid boleto", mutate: func(p *requestPaymentPayload) { p.BoletoCode = "123" }, wantErr: "boletoCode"},
		{name: "insecure link", mutate: func(p *requestPaymentPayload) { p.PaymentLink = "http://example.com/pay" }, wantErr: "paymentLink"},
		{name: "missing method", mutate: func(p *requestPaymentPayload) { p.PixKey = "" }, wantErr: "at least one payment method"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := validPaymentPayload()
			test.mutate(&payload)
			err := normalizePaymentPayload(&payload, "GENERATED-ID")
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
			}
		})
	}
}
