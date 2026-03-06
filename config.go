package stripe

import (
	"fmt"
	"strings"
	"time"
)

// UIConfig configures client-facing Stripe Elements defaults used by the
// render-pure island helper methods on *UI.
//
// IMPORTANT: UIConfig MUST NOT contain secret material.
// Everything in UIConfig is either publishable (pk_*) or non-secret UI defaults.
type UIConfig struct {
	// PublishableKey is the Stripe publishable key (pk_live_* or pk_test_*).
	// REQUIRED.
	PublishableKey string

	// Locale is the default locale for Stripe Elements (e.g., "en", "fr", "auto").
	// Default: "auto".
	Locale string

	// Appearance is the default Stripe Elements appearance configuration.
	// Default: nil (Stripe default theme).
	Appearance *ElementsAppearance
}

func (c UIConfig) locale() string {
	if c.Locale != "" {
		return c.Locale
	}
	return "auto"
}

// UI is the vango-stripe island renderer/config holder.
// Construct once during app init and pass it via deps/DI.
type UI struct {
	cfg UIConfig
}

func NewUI(cfg UIConfig) (*UI, error) {
	if cfg.PublishableKey == "" {
		return nil, fmt.Errorf("stripe: UIConfig.PublishableKey is required")
	}
	// Defensive: prevent accidental secret injection into client props.
	if !strings.HasPrefix(cfg.PublishableKey, "pk_") {
		// Do not echo the provided value to avoid leaking accidental secret material.
		return nil, fmt.Errorf("stripe: UIConfig.PublishableKey must be a publishable key (pk_*)")
	}
	return &UI{cfg: cfg}, nil
}

func MustNewUI(cfg UIConfig) *UI {
	ui, err := NewUI(cfg)
	if err != nil {
		panic(err)
	}
	return ui
}

// WebhookConfig configures the Stripe webhook handler.
// This config contains secret material and must never be logged.
type WebhookConfig struct {
	// Secret is the Stripe webhook endpoint signing secret (whsec_*). REQUIRED.
	Secret string

	// Tolerance is the maximum age of a webhook event before it is rejected.
	// Default: 300 seconds (Stripe standard tolerance).
	Tolerance time.Duration

	// MaxBodyBytes caps the raw request body read for signature verification.
	// Default: 1MB.
	MaxBodyBytes int64

	// ExpectedLivemode, if set, requires event.Livemode to match.
	// This helps prevent test-mode events hitting live endpoints (and vice versa).
	// Default: nil (no check).
	ExpectedLivemode *bool
}

const DefaultWebhookTolerance = 300 * time.Second
const DefaultWebhookMaxBodyBytes = 1 << 20 // 1MB

func (c WebhookConfig) tolerance() time.Duration {
	if c.Tolerance > 0 {
		return c.Tolerance
	}
	return DefaultWebhookTolerance
}

func (c WebhookConfig) maxBodyBytes() int64 {
	if c.MaxBodyBytes > 0 {
		return c.MaxBodyBytes
	}
	return DefaultWebhookMaxBodyBytes
}
