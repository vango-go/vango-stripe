package stripe

import "testing"

func TestExpressCheckoutElement_RenderContractAndDefaults(t *testing.T) {
	ui := MustNewUI(UIConfig{
		PublishableKey: "pk_test_default",
		Locale:         "fr",
		Appearance: &ElementsAppearance{
			Theme: "night",
		},
	})

	node := ui.ExpressCheckoutElement(ExpressCheckoutProps{
		ClientSecret: "pi_123_secret_abc",
		ReturnURL:    "https://example.com/return",
		ID:           "express-element",
	})

	if got := node.Tag; got != "div" {
		t.Fatalf("expected div, got %q", got)
	}
	if got := node.Props["data-island"]; got != "stripe-express-checkout" {
		t.Fatalf("expected stripe-express-checkout island id, got %#v", got)
	}
	if got := node.Props["id"]; got != "express-element" {
		t.Fatalf("expected id=express-element, got %#v", got)
	}
	mustHaveClass(t, node, "vango-stripe-express-checkout")

	if len(node.Children) != 1 {
		t.Fatalf("expected one placeholder child, got %d", len(node.Children))
	}
	placeholder := node.Children[0]
	if got := placeholder.Props["data-island-placeholder"]; got != "" {
		t.Fatalf("expected placeholder marker, got %#v", got)
	}
	if len(placeholder.Children) != 1 || placeholder.Children[0].Text != "Loading wallets..." {
		t.Fatalf("unexpected placeholder content")
	}

	props := decodeDataProps(t, node)
	if got := props["clientSecret"]; got != "pi_123_secret_abc" {
		t.Fatalf("unexpected clientSecret: %#v", got)
	}
	if got := props["returnURL"]; got != "https://example.com/return" {
		t.Fatalf("unexpected returnURL: %#v", got)
	}
	if got := props["publishableKey"]; got != "pk_test_default" {
		t.Fatalf("expected default publishable key, got %#v", got)
	}
	if got := props["locale"]; got != "fr" {
		t.Fatalf("expected default locale fr, got %#v", got)
	}
	appearance, ok := props["appearance"].(map[string]any)
	if !ok {
		t.Fatalf("expected appearance object, got %T", props["appearance"])
	}
	if got := appearance["theme"]; got != "night" {
		t.Fatalf("expected appearance.theme=night, got %#v", got)
	}
	if _, exists := props["id"]; exists {
		t.Fatalf("ID must not be serialized in island props")
	}
}

func TestExpressCheckoutElement_ExplicitOverrides(t *testing.T) {
	ui := MustNewUI(UIConfig{
		PublishableKey: "pk_test_default",
		Locale:         "auto",
	})

	node := ui.ExpressCheckoutElement(ExpressCheckoutProps{
		ClientSecret:   "pi_456_secret_def",
		ReturnURL:      "https://example.com/return",
		PublishableKey: "pk_test_override",
		RemountKey:     "stable-remount-key",
		Locale:         "de",
		Appearance:     &ElementsAppearance{Theme: "flat"},
		ButtonType:     "checkout",
		ButtonTheme:    "outline",
		ButtonHeight:   55,
		Wallets: &ExpressCheckoutWallets{
			ApplePay:  "auto",
			GooglePay: "never",
			Link:      "auto",
		},
	})
	props := decodeDataProps(t, node)

	if got := props["publishableKey"]; got != "pk_test_override" {
		t.Fatalf("expected override publishable key, got %#v", got)
	}
	if got := props["remountKey"]; got != "stable-remount-key" {
		t.Fatalf("expected remountKey, got %#v", got)
	}
	if got := props["locale"]; got != "de" {
		t.Fatalf("expected locale=de, got %#v", got)
	}
	appearance, ok := props["appearance"].(map[string]any)
	if !ok || appearance["theme"] != "flat" {
		t.Fatalf("expected override appearance, got %#v", props["appearance"])
	}
	if got := props["buttonType"]; got != "checkout" {
		t.Fatalf("expected buttonType=checkout, got %#v", got)
	}
	if got := props["buttonTheme"]; got != "outline" {
		t.Fatalf("expected buttonTheme=outline, got %#v", got)
	}
	if got := props["buttonHeight"]; got != float64(55) {
		t.Fatalf("expected buttonHeight=55, got %#v", got)
	}
	wallets, ok := props["wallets"].(map[string]any)
	if !ok {
		t.Fatalf("expected wallets object, got %T", props["wallets"])
	}
	if wallets["applePay"] != "auto" || wallets["googlePay"] != "never" || wallets["link"] != "auto" {
		t.Fatalf("unexpected wallets payload: %#v", wallets)
	}
}

func TestExpressCheckoutElement_DeterministicAndInputNotMutated(t *testing.T) {
	ui := MustNewUI(UIConfig{
		PublishableKey: "pk_test_default",
		Locale:         "en",
		Appearance:     &ElementsAppearance{Theme: "night"},
	})
	input := ExpressCheckoutProps{
		ClientSecret: "pi_123_secret_abc",
		ReturnURL:    "https://example.com/return",
	}

	node1 := ui.ExpressCheckoutElement(input)
	node2 := ui.ExpressCheckoutElement(input)

	if got1, got2 := node1.Props["data-props"], node2.Props["data-props"]; got1 != got2 {
		t.Fatalf("expected deterministic data-props; got %q != %q", got1, got2)
	}
	if input.PublishableKey != "" || input.Locale != "" || input.Appearance != nil {
		t.Fatalf("input props mutated: %#v", input)
	}
}

func TestExpressCheckoutElement_ValidationPanics(t *testing.T) {
	ui := MustNewUI(UIConfig{PublishableKey: "pk_test_123"})
	valid := ExpressCheckoutProps{
		ClientSecret: "pi_123_secret_abc",
		ReturnURL:    "https://example.com/return",
	}

	tests := []struct {
		name string
		want string
		fn   func()
	}{
		{
			name: "nil ui",
			want: "nil UI",
			fn: func() {
				var nilUI *UI
				_ = nilUI.ExpressCheckoutElement(valid)
			},
		},
		{
			name: "missing client secret",
			want: "ClientSecret is required",
			fn: func() {
				p := valid
				p.ClientSecret = ""
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "checkout session secret is rejected",
			want: "Checkout Session (cs_*)",
			fn: func() {
				p := valid
				p.ClientSecret = "cs_test_123"
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid client secret shape is rejected",
			want: "pi_*_secret_*",
			fn: func() {
				p := valid
				p.ClientSecret = "seti_123_secret_abc"
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "missing return url",
			want: "ReturnURL is required",
			fn: func() {
				p := valid
				p.ReturnURL = ""
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "non absolute return url",
			want: "ReturnURL must be an absolute URL",
			fn: func() {
				p := valid
				p.ReturnURL = "/checkout/complete"
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid publishable key override",
			want: "PublishableKey must be a publishable key (pk_*)",
			fn: func() {
				p := valid
				p.PublishableKey = "sk_test_123"
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "missing publishable key in prop and ui config",
			want: "PublishableKey is required",
			fn: func() {
				_ = (&UI{}).ExpressCheckoutElement(valid)
			},
		},
		{
			name: "button height too small",
			want: "ButtonHeight out of range",
			fn: func() {
				p := valid
				p.ButtonHeight = 39
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "button height too large",
			want: "ButtonHeight out of range",
			fn: func() {
				p := valid
				p.ButtonHeight = 56
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid button type",
			want: "invalid ButtonType",
			fn: func() {
				p := valid
				p.ButtonType = "invalid"
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid button theme",
			want: "invalid ButtonTheme",
			fn: func() {
				p := valid
				p.ButtonTheme = "invalid"
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid wallets apple pay",
			want: "invalid Wallets.ApplePay",
			fn: func() {
				p := valid
				p.Wallets = &ExpressCheckoutWallets{ApplePay: "always"}
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid wallets google pay",
			want: "invalid Wallets.GooglePay",
			fn: func() {
				p := valid
				p.Wallets = &ExpressCheckoutWallets{GooglePay: "always"}
				_ = ui.ExpressCheckoutElement(p)
			},
		},
		{
			name: "invalid wallets link",
			want: "invalid Wallets.Link",
			fn: func() {
				p := valid
				p.Wallets = &ExpressCheckoutWallets{Link: "always"}
				_ = ui.ExpressCheckoutElement(p)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustPanic(t, tt.fn, tt.want)
		})
	}
}

func TestExpressCheckoutElement_ValidOptionsDoNotPanic(t *testing.T) {
	ui := MustNewUI(UIConfig{PublishableKey: "pk_test_123"})

	mustNotPanic(t, func() {
		_ = ui.ExpressCheckoutElement(ExpressCheckoutProps{
			ClientSecret: "pi_123_secret_abc",
			ReturnURL:    "https://example.com/return",
			ButtonType:   "subscribe",
			ButtonTheme:  "dark",
			ButtonHeight: 40,
			Wallets: &ExpressCheckoutWallets{
				ApplePay:  "auto",
				GooglePay: "never",
				Link:      "auto",
			},
		})
	})

	mustNotPanic(t, func() {
		_ = ui.ExpressCheckoutElement(ExpressCheckoutProps{
			ClientSecret: "pi_123_secret_abc",
			ReturnURL:    "https://example.com/return",
			ButtonHeight: 55,
		})
	})
}
