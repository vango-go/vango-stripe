package stripe

import "testing"

func TestPaymentElement_RenderContractAndDefaults(t *testing.T) {
	ui := MustNewUI(UIConfig{
		PublishableKey: "pk_test_default",
		Locale:         "fr",
		Appearance: &ElementsAppearance{
			Theme: "night",
		},
	})

	node := ui.PaymentElement(PaymentElementProps{
		ClientSecret: "pi_123_secret_abc",
		ReturnURL:    "https://example.com/return",
		ID:           "payment-element",
	})

	if got := node.Tag; got != "div" {
		t.Fatalf("expected div, got %q", got)
	}
	if got := node.Props["data-island"]; got != "stripe-payment-element" {
		t.Fatalf("expected stripe-payment-element island id, got %#v", got)
	}
	if got := node.Props["id"]; got != "payment-element" {
		t.Fatalf("expected id=payment-element, got %#v", got)
	}
	mustHaveClass(t, node, "vango-stripe-payment-element")

	if len(node.Children) != 1 {
		t.Fatalf("expected one placeholder child, got %d", len(node.Children))
	}
	placeholder := node.Children[0]
	if got := placeholder.Props["data-island-placeholder"]; got != "" {
		t.Fatalf("expected placeholder marker, got %#v", got)
	}
	if len(placeholder.Children) != 1 || placeholder.Children[0].Text != "Loading payment..." {
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

func TestPaymentElement_ExplicitOverrides(t *testing.T) {
	ui := MustNewUI(UIConfig{
		PublishableKey: "pk_test_default",
		Locale:         "auto",
		Appearance: &ElementsAppearance{
			Theme: "night",
		},
	})

	node := ui.PaymentElement(PaymentElementProps{
		ClientSecret:        "pi_456_secret_def",
		ReturnURL:           "https://example.com/return",
		PublishableKey:      "pk_test_override",
		RemountKey:          "stable-remount-key",
		Layout:              "tabs",
		Locale:              "de",
		Appearance:          &ElementsAppearance{Theme: "flat"},
		Business:            &ElementsBusiness{Name: "Acme Inc"},
		EmitChangeEvents:    true,
		DisableSubmitButton: true,
		SubmitButtonText:    "Pay now",
	})
	props := decodeDataProps(t, node)

	if got := props["publishableKey"]; got != "pk_test_override" {
		t.Fatalf("expected override publishable key, got %#v", got)
	}
	if got := props["remountKey"]; got != "stable-remount-key" {
		t.Fatalf("expected remountKey, got %#v", got)
	}
	if got := props["layout"]; got != "tabs" {
		t.Fatalf("expected layout=tabs, got %#v", got)
	}
	if got := props["locale"]; got != "de" {
		t.Fatalf("expected locale=de, got %#v", got)
	}
	appearance, ok := props["appearance"].(map[string]any)
	if !ok || appearance["theme"] != "flat" {
		t.Fatalf("expected override appearance, got %#v", props["appearance"])
	}
	business, ok := props["business"].(map[string]any)
	if !ok || business["name"] != "Acme Inc" {
		t.Fatalf("expected business payload, got %#v", props["business"])
	}
	if got := props["emitChangeEvents"]; got != true {
		t.Fatalf("expected emitChangeEvents=true, got %#v", got)
	}
	if got := props["disableSubmitButton"]; got != true {
		t.Fatalf("expected disableSubmitButton=true, got %#v", got)
	}
	if got := props["submitButtonText"]; got != "Pay now" {
		t.Fatalf("expected submitButtonText, got %#v", got)
	}
}

func TestPaymentElement_DeterministicAndInputNotMutated(t *testing.T) {
	ui := MustNewUI(UIConfig{
		PublishableKey: "pk_test_default",
		Locale:         "en",
		Appearance:     &ElementsAppearance{Theme: "night"},
	})
	input := PaymentElementProps{
		ClientSecret: "pi_123_secret_abc",
		ReturnURL:    "https://example.com/return",
	}

	node1 := ui.PaymentElement(input)
	node2 := ui.PaymentElement(input)

	if got1, got2 := node1.Props["data-props"], node2.Props["data-props"]; got1 != got2 {
		t.Fatalf("expected deterministic data-props; got %q != %q", got1, got2)
	}
	if input.PublishableKey != "" || input.Locale != "" || input.Appearance != nil {
		t.Fatalf("input props mutated: %#v", input)
	}
}

func TestPaymentElement_ValidationPanics(t *testing.T) {
	ui := MustNewUI(UIConfig{PublishableKey: "pk_test_123"})
	valid := PaymentElementProps{
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
				_ = nilUI.PaymentElement(valid)
			},
		},
		{
			name: "missing client secret",
			want: "ClientSecret is required",
			fn: func() {
				p := valid
				p.ClientSecret = ""
				_ = ui.PaymentElement(p)
			},
		},
		{
			name: "checkout session secret is rejected",
			want: "Checkout Session (cs_*)",
			fn: func() {
				p := valid
				p.ClientSecret = "cs_test_123"
				_ = ui.PaymentElement(p)
			},
		},
		{
			name: "invalid client secret shape is rejected",
			want: "pi_*_secret_*",
			fn: func() {
				p := valid
				p.ClientSecret = "seti_123_secret_abc"
				_ = ui.PaymentElement(p)
			},
		},
		{
			name: "missing return url",
			want: "ReturnURL is required",
			fn: func() {
				p := valid
				p.ReturnURL = ""
				_ = ui.PaymentElement(p)
			},
		},
		{
			name: "non absolute return url",
			want: "ReturnURL must be an absolute URL",
			fn: func() {
				p := valid
				p.ReturnURL = "/checkout/complete"
				_ = ui.PaymentElement(p)
			},
		},
		{
			name: "invalid publishable key override",
			want: "PublishableKey must be a publishable key (pk_*)",
			fn: func() {
				p := valid
				p.PublishableKey = "sk_test_123"
				_ = ui.PaymentElement(p)
			},
		},
		{
			name: "missing publishable key in prop and ui config",
			want: "PublishableKey is required",
			fn: func() {
				_ = (&UI{}).PaymentElement(valid)
			},
		},
		{
			name: "invalid layout",
			want: "invalid Layout",
			fn: func() {
				p := valid
				p.Layout = "grid"
				_ = ui.PaymentElement(p)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustPanic(t, tt.fn, tt.want)
		})
	}
}

func TestPaymentElement_ValidLayoutsDoNotPanic(t *testing.T) {
	ui := MustNewUI(UIConfig{PublishableKey: "pk_test_123"})
	layouts := []string{"", "auto", "tabs", "accordion"}

	for _, layout := range layouts {
		layout := layout
		t.Run(layoutOrDefault(layout), func(t *testing.T) {
			mustNotPanic(t, func() {
				_ = ui.PaymentElement(PaymentElementProps{
					ClientSecret: "pi_123_secret_abc",
					ReturnURL:    "https://example.com/return",
					Layout:       layout,
				})
			})
		})
	}
}

func layoutOrDefault(layout string) string {
	if layout == "" {
		return "empty-layout"
	}
	return layout
}
