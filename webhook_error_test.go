package stripe

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookHandler_HandlerErrorMapsStatusAndMessage(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_test_002", "payment_intent.succeeded", false, map[string]any{"id": "pi_123"})
	sigHeader := stripeSignatureHeader(time.Now(), secret, payload)

	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute},
		On("payment_intent.succeeded", func(ctx *EventContext) error {
			return &HandlerError{StatusCode: http.StatusServiceUnavailable, Message: "maintenance"}
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sigHeader)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%q", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "maintenance") {
		t.Fatalf("expected maintenance message, got %q", rr.Body.String())
	}
}

func TestWebhookHandler_HandlerErrorInvalidStatusFallsBack500(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_test_003", "payment_intent.succeeded", false, map[string]any{"id": "pi_123"})
	sigHeader := stripeSignatureHeader(time.Now(), secret, payload)

	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute},
		On("payment_intent.succeeded", func(ctx *EventContext) error {
			return &HandlerError{StatusCode: 42, Message: "invalid status"}
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sigHeader)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%q", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Internal error") {
		t.Fatalf("expected internal error body, got %q", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "invalid status") {
		t.Fatalf("unexpected leak of invalid message in fallback body: %q", rr.Body.String())
	}
}

func TestWebhookHandler_GenericErrorReturns500WithoutLeak(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_test_004", "payment_intent.succeeded", false, map[string]any{"id": "pi_123"})
	sigHeader := stripeSignatureHeader(time.Now(), secret, payload)

	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute},
		On("payment_intent.succeeded", func(ctx *EventContext) error {
			return errors.New("db failed: sk_test_abc123 whsec_hidden")
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sigHeader)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%q", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Internal error") {
		t.Fatalf("expected internal error body, got %q", body)
	}
	if strings.Contains(body, "sk_test_") || strings.Contains(body, "whsec_") {
		t.Fatalf("secret token leaked in response body: %q", body)
	}
}
