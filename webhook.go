package stripe

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	stripelib "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

// EventContext is passed to handlers and includes the verified Stripe event plus HTTP context.
type EventContext struct {
	Event     stripelib.Event
	Request   *http.Request
	RawBody   []byte
	Signature string // Stripe-Signature header value
}

// EventHandler handles a verified Stripe webhook event.
// Returning nil indicates success and yields HTTP 200.
type EventHandler func(*EventContext) error

// HandlerError allows handlers to control the HTTP response code.
// Message is used as the public HTTP response body.
type HandlerError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *HandlerError) Error() string {
	// Do not format/concatenate wrapped errors into the primary error string.
	// HandlerError is commonly logged by applications, and wrapped errors can
	// contain sensitive tokens or request context. Callers that want details
	// can log e.Err explicitly (carefully) or use errors.Unwrap.
	if e.Message == "" {
		return "stripe webhook: handler error"
	}
	return fmt.Sprintf("stripe webhook: %s", e.Message)
}

func (e *HandlerError) Unwrap() error { return e.Err }

// EventRegistration maps an event type to its handler.
type EventRegistration struct {
	EventType string
	Handler   EventHandler
}

// On creates an EventRegistration for the given Stripe event type.
func On(eventType string, handler EventHandler) EventRegistration {
	return EventRegistration{EventType: eventType, Handler: handler}
}

// WebhookHandler returns an http.Handler that:
//   - reads raw bytes (bounded),
//   - verifies Stripe signatures using the raw bytes, and
//   - dispatches to a typed handler by event type.
func WebhookHandler(cfg WebhookConfig, registrations ...EventRegistration) http.Handler {
	if cfg.Secret == "" {
		panic("stripe.WebhookHandler: Secret is required")
	}
	tolerance := cfg.tolerance()
	maxBody := cfg.maxBodyBytes()
	expectedLivemode := cfg.ExpectedLivemode

	handlers := make(map[string]EventHandler, len(registrations))
	for _, r := range registrations {
		if r.EventType == "" {
			panic("stripe.WebhookHandler: empty event type")
		}
		if r.Handler == nil {
			panic(fmt.Sprintf("stripe.WebhookHandler: nil handler for %q", r.EventType))
		}
		if _, exists := handlers[r.EventType]; exists {
			panic(fmt.Sprintf("stripe.WebhookHandler: duplicate handler for %q", r.EventType))
		}
		handlers[r.EventType] = r.Handler
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		sigHeader := r.Header.Get("Stripe-Signature")
		if sigHeader == "" {
			http.Error(w, "Missing Stripe-Signature header", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		if int64(len(body)) > maxBody {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		event, err := webhook.ConstructEventWithTolerance(body, sigHeader, cfg.Secret, tolerance)
		if err != nil {
			http.Error(w, "Invalid signature", http.StatusBadRequest)
			return
		}

		if expectedLivemode != nil && event.Livemode != *expectedLivemode {
			http.Error(w, "Invalid livemode", http.StatusBadRequest)
			return
		}

		handler, exists := handlers[string(event.Type)]
		if !exists {
			w.WriteHeader(http.StatusOK)
			return
		}

		ctx := &EventContext{
			Event:     event,
			Request:   r,
			RawBody:   body,
			Signature: sigHeader,
		}

		if err := handler(ctx); err != nil {
			var he *HandlerError
			if errors.As(err, &he) {
				statusCode := he.StatusCode
				message := he.Message
				if statusCode < 100 || statusCode > 999 {
					statusCode = http.StatusInternalServerError
					message = "Internal error"
				}
				http.Error(w, message, statusCode)
			} else {
				http.Error(w, "Internal error", http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusOK)
	})
}
