package stripe

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

var secretPattern = regexp.MustCompile(`(?:sk_(?:test|live)_[A-Za-z0-9]+|whsec_[A-Za-z0-9]+)`)

func TestWebhookHandler_ResponseBodiesDoNotLeakConfiguredSecret(t *testing.T) {
	secret := "whsec_secret_very_sensitive"

	t.Run("missing signature", func(t *testing.T) {
		h := WebhookHandler(WebhookConfig{Secret: secret})
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{}`)))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assertNoSecretsInBody(t, rr.Body.String(), secret)
	})

	t.Run("invalid signature", func(t *testing.T) {
		payload := stripeEventPayload(t, "evt_100", "payment_intent.succeeded", false, map[string]any{"id": "pi_100"})
		h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute})
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
		req.Header.Set("Stripe-Signature", "t=1700000000,v1=deadbeef")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assertNoSecretsInBody(t, rr.Body.String(), secret)
	})

	t.Run("request too large", func(t *testing.T) {
		h := WebhookHandler(WebhookConfig{Secret: secret, MaxBodyBytes: 2})
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte("123")))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assertNoSecretsInBody(t, rr.Body.String(), secret)
	})

	t.Run("method not allowed", func(t *testing.T) {
		h := WebhookHandler(WebhookConfig{Secret: secret})
		req := httptest.NewRequest(http.MethodGet, "/webhooks/stripe", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assertNoSecretsInBody(t, rr.Body.String(), secret)
	})

	t.Run("livemode mismatch", func(t *testing.T) {
		payload := stripeEventPayload(t, "evt_101", "payment_intent.succeeded", true, map[string]any{"id": "pi_101"})
		sig := stripeSignatureHeader(time.Now(), secret, payload)
		expectedLive := false
		h := WebhookHandler(WebhookConfig{Secret: secret, ExpectedLivemode: &expectedLive, Tolerance: 5 * time.Minute})
		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
		req.Header.Set("Stripe-Signature", sig)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assertNoSecretsInBody(t, rr.Body.String(), secret)
	})
}

func TestWebhookHandler_InternalErrorResponseDoesNotLeakSecrets(t *testing.T) {
	secret := "whsec_secret_very_sensitive"
	payload := stripeEventPayload(t, "evt_102", "payment_intent.succeeded", false, map[string]any{"id": "pi_102"})
	sig := stripeSignatureHeader(time.Now(), secret, payload)

	h := WebhookHandler(WebhookConfig{Secret: secret, Tolerance: 5 * time.Minute},
		On("payment_intent.succeeded", func(ctx *EventContext) error {
			return errors.New("db unavailable sk_test_inline whsec_inline")
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assertNoSecretsInBody(t, rr.Body.String(), secret)
}

func assertNoSecretsInBody(t *testing.T, body string, configuredSecret string) {
	t.Helper()
	if strings.Contains(body, configuredSecret) {
		t.Fatalf("configured secret leaked in response body: %q", body)
	}
	if secretPattern.MatchString(body) {
		t.Fatalf("secret-like token leaked in response body: %q", body)
	}
}
