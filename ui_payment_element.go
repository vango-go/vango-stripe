package stripe

import (
	"net/url"
	"strings"

	"github.com/vango-go/vango"
	el "github.com/vango-go/vango/el"
)

// PaymentElementProps configures the Payment Element island.
// This is the canonical vango-stripe model and requires a PaymentIntent client secret.
type PaymentElementProps struct {
	// ClientSecret is the client secret from a PaymentIntent.
	// REQUIRED. Must look like pi_*_secret_* (NOT a Checkout Session client secret).
	ClientSecret string `json:"clientSecret"`

	// ReturnURL is the URL Stripe redirects to after payment confirmation.
	// REQUIRED. Must be an absolute URL.
	ReturnURL string `json:"returnURL"`

	// PublishableKey overrides the Stripe publishable key for this element.
	// If empty, uses UIConfig.PublishableKey from the *UI instance.
	PublishableKey string `json:"publishableKey,omitempty"`

	// RemountKey optionally overrides the island remount decision.
	// If set, the client-side remountKey(props) returns this value directly.
	RemountKey string `json:"remountKey,omitempty"`

	// Layout controls the Payment Element layout.
	// One of: "tabs", "accordion", "auto". Default: "auto".
	Layout string `json:"layout,omitempty"`

	// Locale overrides the locale. If empty, uses UIConfig.Locale or "auto".
	Locale string `json:"locale,omitempty"`

	// Appearance overrides the appearance. If nil, uses UIConfig.Appearance.
	Appearance *ElementsAppearance `json:"appearance,omitempty"`

	// Business is optional business information shown in the Element.
	Business *ElementsBusiness `json:"business,omitempty"`

	// EmitChangeEvents controls whether the island emits high-frequency "change" events.
	// Default: false.
	EmitChangeEvents bool `json:"emitChangeEvents,omitempty"`

	// DisableSubmitButton disables the built-in confirm button.
	// When true, the island will not call stripe.confirmPayment() (mount-only mode).
	DisableSubmitButton bool `json:"disableSubmitButton,omitempty"`

	// SubmitButtonText customizes the submit button label. Default: "Pay now".
	SubmitButtonText string `json:"submitButtonText,omitempty"`

	// ID is an optional DOM ID for the island container.
	// If empty, no ID attribute is set.
	ID string `json:"-"`
}

// PaymentElement renders a Vango Island that mounts the Stripe Payment Element.
//
// This function is render-pure: it produces deterministic VNode output for the same
// inputs and performs no side effects.
func (ui *UI) PaymentElement(p PaymentElementProps, attrs ...vango.Attr) *vango.VNode {
	if ui == nil {
		panic("stripe.UI.PaymentElement: nil UI")
	}
	if p.ClientSecret == "" {
		panic("stripe.UI.PaymentElement: ClientSecret is required")
	}
	// Defensive: prevent mixing Stripe Checkout Sessions with Elements.
	if strings.HasPrefix(p.ClientSecret, "cs_") {
		panic("stripe.UI.PaymentElement: ClientSecret looks like a Checkout Session (cs_*). Use a PaymentIntent client secret.")
	}
	if !strings.HasPrefix(p.ClientSecret, "pi_") || !strings.Contains(p.ClientSecret, "_secret_") {
		panic("stripe.UI.PaymentElement: ClientSecret must be a PaymentIntent client secret (pi_*_secret_*). SetupIntents and other intent kinds are not supported in v1.")
	}
	if p.ReturnURL == "" {
		panic("stripe.UI.PaymentElement: ReturnURL is required")
	}
	if u, err := url.Parse(p.ReturnURL); err != nil || !u.IsAbs() {
		panic("stripe.UI.PaymentElement: ReturnURL must be an absolute URL")
	}

	// Apply defaults from UI config (pure reads only).
	if p.PublishableKey == "" {
		p.PublishableKey = ui.cfg.PublishableKey
	}
	if p.PublishableKey == "" {
		panic("stripe.UI.PaymentElement: PublishableKey is required (prop or UI config)")
	}
	if !strings.HasPrefix(p.PublishableKey, "pk_") {
		panic("stripe.UI.PaymentElement: PublishableKey must be a publishable key (pk_*), not a secret or other key type")
	}
	if p.Layout != "" && p.Layout != "auto" && p.Layout != "tabs" && p.Layout != "accordion" {
		panic("stripe.UI.PaymentElement: invalid Layout (expected auto|tabs|accordion)")
	}
	if p.Locale == "" {
		p.Locale = ui.cfg.locale()
	}
	if p.Appearance == nil && ui.cfg.Appearance != nil {
		p.Appearance = ui.cfg.Appearance
	}

	base := []any{
		el.Class("vango-stripe-payment-element"),
		el.JSIsland("stripe-payment-element", p),
		// SSR-only placeholder (opaque subtree; Vango will not patch inside after mount).
		el.IslandPlaceholder(el.Text("Loading payment...")),
	}
	if p.ID != "" {
		base = append([]any{el.ID(p.ID)}, base...)
	}
	for _, a := range attrs {
		base = append(base, a)
	}
	return el.Div(base...)
}
