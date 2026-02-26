package stripe

import (
	"net/url"
	"strings"

	"github.com/vango-go/vango"
	el "github.com/vango-go/vango/el"
)

// ExpressCheckoutProps configures the Express Checkout Element island.
type ExpressCheckoutProps struct {
	// ClientSecret from a PaymentIntent. REQUIRED.
	ClientSecret string `json:"clientSecret"`

	// ReturnURL for post-confirmation redirect. REQUIRED. Absolute URL.
	ReturnURL string `json:"returnURL"`

	// PublishableKey override. If empty, uses UIConfig.PublishableKey from the *UI instance.
	PublishableKey string `json:"publishableKey,omitempty"`

	// RemountKey optionally overrides the island remount decision.
	// If set, the client-side remountKey(props) returns this value directly.
	RemountKey string `json:"remountKey,omitempty"`

	// Locale override. If empty, uses UIConfig.Locale or "auto".
	Locale string `json:"locale,omitempty"`

	// Appearance override. If nil, uses UIConfig.Appearance.
	Appearance *ElementsAppearance `json:"appearance,omitempty"`

	// ButtonType: "buy", "pay", "book", "donate", "checkout", "subscribe", "plain".
	// Default: "buy".
	ButtonType string `json:"buttonType,omitempty"`

	// ButtonTheme: "dark", "light", "outline". Default: Stripe auto-selects.
	ButtonTheme string `json:"buttonTheme,omitempty"`

	// ButtonHeight in pixels. Range: 40-55. Default: 44.
	ButtonHeight int `json:"buttonHeight,omitempty"`

	// Wallets controls which wallets are displayed. If nil, all shown.
	Wallets *ExpressCheckoutWallets `json:"wallets,omitempty"`

	// ID is an optional DOM ID. If empty, no ID is set.
	ID string `json:"-"`
}

// ExpressCheckoutElement renders a Vango Island that mounts the Stripe Express Checkout Element.
//
// This function is render-pure.
func (ui *UI) ExpressCheckoutElement(p ExpressCheckoutProps, attrs ...vango.Attr) *vango.VNode {
	if ui == nil {
		panic("stripe.UI.ExpressCheckoutElement: nil UI")
	}
	if p.ClientSecret == "" {
		panic("stripe.UI.ExpressCheckoutElement: ClientSecret is required")
	}
	if strings.HasPrefix(p.ClientSecret, "cs_") {
		panic("stripe.UI.ExpressCheckoutElement: ClientSecret looks like a Checkout Session (cs_*). Use a PaymentIntent client secret.")
	}
	if !strings.HasPrefix(p.ClientSecret, "pi_") || !strings.Contains(p.ClientSecret, "_secret_") {
		panic("stripe.UI.ExpressCheckoutElement: ClientSecret must be a PaymentIntent client secret (pi_*_secret_*). SetupIntents and other intent kinds are not supported in v1.")
	}
	if p.ReturnURL == "" {
		panic("stripe.UI.ExpressCheckoutElement: ReturnURL is required")
	}
	if u, err := url.Parse(p.ReturnURL); err != nil || !u.IsAbs() {
		panic("stripe.UI.ExpressCheckoutElement: ReturnURL must be an absolute URL")
	}

	if p.PublishableKey == "" {
		p.PublishableKey = ui.cfg.PublishableKey
	}
	if p.PublishableKey == "" {
		panic("stripe.UI.ExpressCheckoutElement: PublishableKey is required (prop or UI config)")
	}
	if !strings.HasPrefix(p.PublishableKey, "pk_") {
		panic("stripe.UI.ExpressCheckoutElement: PublishableKey must be a publishable key (pk_*), not a secret or other key type")
	}
	if p.ButtonHeight != 0 && (p.ButtonHeight < 40 || p.ButtonHeight > 55) {
		panic("stripe.UI.ExpressCheckoutElement: ButtonHeight out of range (40-55)")
	}
	if p.ButtonType != "" {
		switch p.ButtonType {
		case "buy", "pay", "book", "donate", "checkout", "subscribe", "plain":
		default:
			panic("stripe.UI.ExpressCheckoutElement: invalid ButtonType")
		}
	}
	if p.ButtonTheme != "" {
		switch p.ButtonTheme {
		case "dark", "light", "outline":
		default:
			panic("stripe.UI.ExpressCheckoutElement: invalid ButtonTheme")
		}
	}
	if p.Wallets != nil {
		if p.Wallets.ApplePay != "" && p.Wallets.ApplePay != "auto" && p.Wallets.ApplePay != "never" {
			panic("stripe.UI.ExpressCheckoutElement: invalid Wallets.ApplePay")
		}
		if p.Wallets.GooglePay != "" && p.Wallets.GooglePay != "auto" && p.Wallets.GooglePay != "never" {
			panic("stripe.UI.ExpressCheckoutElement: invalid Wallets.GooglePay")
		}
		if p.Wallets.Link != "" && p.Wallets.Link != "auto" && p.Wallets.Link != "never" {
			panic("stripe.UI.ExpressCheckoutElement: invalid Wallets.Link")
		}
	}
	if p.Locale == "" {
		p.Locale = ui.cfg.locale()
	}
	if p.Appearance == nil && ui.cfg.Appearance != nil {
		p.Appearance = ui.cfg.Appearance
	}

	base := []any{
		el.Class("vango-stripe-express-checkout"),
		el.JSIsland("stripe-express-checkout", p),
		el.IslandPlaceholder(el.Text("Loading wallets...")),
	}
	if p.ID != "" {
		base = append([]any{el.ID(p.ID)}, base...)
	}
	for _, a := range attrs {
		base = append(base, a)
	}
	return el.Div(base...)
}
