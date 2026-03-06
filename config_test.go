package stripe

import (
	"strings"
	"testing"
	"time"
)

func TestNewUI_RejectsEmptyPublishableKey(t *testing.T) {
	_, err := NewUI(UIConfig{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "UIConfig.PublishableKey is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewUI_RejectsNonPublishableKey(t *testing.T) {
	_, err := NewUI(UIConfig{PublishableKey: "sk_test_123"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "publishable key (pk_*)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewUI_RejectsNonPublishableKey_DoesNotLeakKeyMaterial(t *testing.T) {
	secretLike := "sk_test_secret123"
	_, err := NewUI(UIConfig{PublishableKey: secretLike})
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, secretLike) {
		t.Fatalf("error leaked key material: %q", msg)
	}
}

func TestNewUI_AcceptsPublishableKeyAndStoresConfig(t *testing.T) {
	appearance := &ElementsAppearance{
		Theme: "night",
		Variables: map[string]string{
			"colorPrimary": "#000000",
		},
	}
	ui, err := NewUI(UIConfig{
		PublishableKey: "pk_test_123",
		Locale:         "fr",
		Appearance:     appearance,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ui == nil {
		t.Fatalf("expected ui")
	}
	if ui.cfg.PublishableKey != "pk_test_123" {
		t.Fatalf("unexpected publishable key: %q", ui.cfg.PublishableKey)
	}
	if ui.cfg.Locale != "fr" {
		t.Fatalf("unexpected locale: %q", ui.cfg.Locale)
	}
	if ui.cfg.Appearance != appearance {
		t.Fatalf("expected appearance pointer to be preserved")
	}
}

func TestMustNewUI_PanicsOnInvalidConfig(t *testing.T) {
	mustPanic(t, func() {
		_ = MustNewUI(UIConfig{PublishableKey: "sk_test_123"})
	}, "publishable key (pk_*)")
}

func TestMustNewUI_ReturnsUIOnValidConfig(t *testing.T) {
	mustNotPanic(t, func() {
		if MustNewUI(UIConfig{PublishableKey: "pk_test_123"}) == nil {
			t.Fatalf("expected non-nil UI")
		}
	})
}

func TestUIConfigLocale_DefaultsToAuto(t *testing.T) {
	var cfg UIConfig
	if got := cfg.locale(); got != "auto" {
		t.Fatalf("expected auto, got %q", got)
	}
}

func TestUIConfigLocale_UsesExplicitValue(t *testing.T) {
	cfg := UIConfig{Locale: "en"}
	if got := cfg.locale(); got != "en" {
		t.Fatalf("expected en, got %q", got)
	}
}

func TestWebhookConfigTolerance_DefaultAndOverride(t *testing.T) {
	var cfg WebhookConfig
	if got := cfg.tolerance(); got != DefaultWebhookTolerance {
		t.Fatalf("expected default tolerance %v, got %v", DefaultWebhookTolerance, got)
	}

	cfg.Tolerance = 90 * time.Second
	if got := cfg.tolerance(); got != 90*time.Second {
		t.Fatalf("expected override tolerance 90s, got %v", got)
	}
}

func TestWebhookConfigMaxBodyBytes_DefaultAndOverride(t *testing.T) {
	var cfg WebhookConfig
	if got := cfg.maxBodyBytes(); got != DefaultWebhookMaxBodyBytes {
		t.Fatalf("expected default max body %d, got %d", DefaultWebhookMaxBodyBytes, got)
	}

	cfg.MaxBodyBytes = 4096
	if got := cfg.maxBodyBytes(); got != 4096 {
		t.Fatalf("expected override max body 4096, got %d", got)
	}
}
