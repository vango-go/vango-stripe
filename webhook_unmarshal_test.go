package stripe

import (
	"encoding/json"
	"strings"
	"testing"

	stripelib "github.com/stripe/stripe-go/v84"
)

func TestUnmarshalEventData_Success(t *testing.T) {
	ctx := &EventContext{
		Event: stripelib.Event{
			Type: stripelib.EventTypePaymentIntentSucceeded,
			Data: &stripelib.EventData{Raw: json.RawMessage(`{"id":"pi_123","amount":2999}`)},
		},
	}

	type paymentIntentData struct {
		ID     string `json:"id"`
		Amount int64  `json:"amount"`
	}

	got, err := UnmarshalEventData[paymentIntentData](ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "pi_123" || got.Amount != 2999 {
		t.Fatalf("unexpected decoded payload: %#v", got)
	}
}

func TestUnmarshalEventData_MalformedData_NoRawLeak(t *testing.T) {
	raw := `{"id": "pi_123"`
	ctx := &EventContext{
		Event: stripelib.Event{
			Type: stripelib.EventTypePaymentIntentSucceeded,
			Data: &stripelib.EventData{Raw: json.RawMessage(raw)},
		},
	}

	_, err := UnmarshalEventData[map[string]any](ctx)
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "payment_intent.succeeded") {
		t.Fatalf("expected event type in error, got %q", msg)
	}
	if strings.Contains(msg, raw) {
		t.Fatalf("raw payload leaked in error: %q", msg)
	}
}

func TestUnmarshalEventData_MissingDataObject(t *testing.T) {
	ctx := &EventContext{
		Event: stripelib.Event{Type: stripelib.EventTypeInvoicePaid},
	}

	_, err := UnmarshalEventData[map[string]any](ctx)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "missing data.object") {
		t.Fatalf("expected missing data.object error, got %q", err.Error())
	}
}

func TestUnmarshalEventData_NilContext(t *testing.T) {
	_, err := UnmarshalEventData[map[string]any](nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "event context is nil") {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}
