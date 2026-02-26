package stripe

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	stripelib "github.com/stripe/stripe-go/v84"
)

func TestWebhookHandler_VerifiesSignatureAndPropagatesContext(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_test_001", "payment_intent.succeeded", false, map[string]any{
		"id": "pi_test_123",
	})
	timestamp := time.Now()
	sigHeader := stripeSignatureHeader(timestamp, secret, payload)

	called := false
	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute},
		On("payment_intent.succeeded", func(ctx *EventContext) error {
			called = true
			if ctx == nil {
				t.Fatalf("expected non-nil context")
			}
			if !bytes.Equal(ctx.RawBody, payload) {
				t.Fatalf("raw body mismatch")
			}
			if ctx.Signature != sigHeader {
				t.Fatalf("signature mismatch: %q != %q", ctx.Signature, sigHeader)
			}
			if ctx.Request == nil {
				t.Fatalf("expected request in context")
			}
			if ctx.Event.ID != "evt_test_001" {
				t.Fatalf("unexpected event id: %q", ctx.Event.ID)
			}
			if string(ctx.Event.Type) != "payment_intent.succeeded" {
				t.Fatalf("unexpected event type: %q", ctx.Event.Type)
			}
			return nil
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sigHeader)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

func TestWebhookHandler_RejectsMissingSignature(t *testing.T) {
	h := WebhookHandler(WebhookConfig{Secret: "whsec_test_123"})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(stripeEventPayload(t, "evt_1", "payment_intent.succeeded", false, map[string]any{"id": "pi_1"})))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestWebhookHandler_RejectsInvalidSignature(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_1", "payment_intent.succeeded", false, map[string]any{"id": "pi_1"})

	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", "t=1700000000,v1=deadbeef")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	h := WebhookHandler(WebhookConfig{Secret: "whsec_test_123"})
	req := httptest.NewRequest(http.MethodGet, "/webhooks/stripe", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
	if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow POST, got %q", allow)
	}
}

func TestWebhookHandler_RequestBodyTooLarge(t *testing.T) {
	h := WebhookHandler(WebhookConfig{Secret: "whsec_test_123", MaxBodyBytes: 8})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte("123456789")))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestWebhookHandler_UnknownEventTypeIs200AndNotDispatched(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_1", "invoice.paid", false, map[string]any{"id": "in_123"})
	sigHeader := stripeSignatureHeader(time.Now(), secret, payload)

	called := false
	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute},
		On("payment_intent.succeeded", func(ctx *EventContext) error {
			called = true
			return nil
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sigHeader)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatalf("expected no dispatch for unknown event type")
	}
}

func TestWebhookHandler_RejectsInvalidLivemode(t *testing.T) {
	secret := "whsec_test_123"
	payload := stripeEventPayload(t, "evt_1", "payment_intent.succeeded", true, map[string]any{"id": "pi_123"})
	sigHeader := stripeSignatureHeader(time.Now(), secret, payload)
	expected := false

	h := WebhookHandler(WebhookConfig{
		Secret:           secret,
		Tolerance:        5 * time.Minute,
		ExpectedLivemode: &expected,
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sigHeader)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestWebhookHandler_PanicsOnDuplicateRegistrations(t *testing.T) {
	mustPanic(t, func() {
		_ = WebhookHandler(WebhookConfig{Secret: "whsec_test_123"},
			On("payment_intent.succeeded", func(ctx *EventContext) error { return nil }),
			On("payment_intent.succeeded", func(ctx *EventContext) error { return nil }),
		)
	}, "duplicate handler")
}

func TestWebhookHandler_PanicsOnEmptyEventType(t *testing.T) {
	mustPanic(t, func() {
		_ = WebhookHandler(WebhookConfig{Secret: "whsec_test_123"},
			On("", func(ctx *EventContext) error { return nil }),
		)
	}, "empty event type")
}

func TestWebhookHandler_PanicsOnNilHandler(t *testing.T) {
	mustPanic(t, func() {
		_ = WebhookHandler(WebhookConfig{Secret: "whsec_test_123"},
			On("payment_intent.succeeded", nil),
		)
	}, "nil handler")
}

func TestWebhookHandler_PanicsOnMissingSecret(t *testing.T) {
	mustPanic(t, func() {
		_ = WebhookHandler(WebhookConfig{})
	}, "Secret is required")
}

func stripeSignatureHeader(ts time.Time, secret string, payload []byte) string {
	timestamp := strconv.FormatInt(ts.Unix(), 10)
	signedPayload := []byte(timestamp + "." + string(payload))

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(signedPayload)
	sig := hex.EncodeToString(mac.Sum(nil))

	return "t=" + timestamp + ",v1=" + sig
}

func stripeEventPayload(t *testing.T, eventID, eventType string, livemode bool, object map[string]any) []byte {
	t.Helper()
	if object == nil {
		object = map[string]any{}
	}

	payload := map[string]any{
		"object":      "event",
		"id":          eventID,
		"type":        eventType,
		"livemode":    livemode,
		"created":     1700000000,
		"api_version": stripelib.APIVersion,
		"data": map[string]any{
			"object": object,
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}
