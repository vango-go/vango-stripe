package stripe

// ElementsAppearance configures the visual appearance of Stripe Elements.
// Maps to Stripe's Appearance API: https://docs.stripe.com/elements/appearance-api
type ElementsAppearance struct {
	Theme     string                       `json:"theme,omitempty"`
	Variables map[string]string            `json:"variables,omitempty"`
	Rules     map[string]map[string]string `json:"rules,omitempty"`
}

// ElementsBusiness contains business information displayed in the Element.
type ElementsBusiness struct {
	Name string `json:"name,omitempty"`
}

// ExpressCheckoutWallets controls wallet button visibility.
type ExpressCheckoutWallets struct {
	ApplePay  string `json:"applePay,omitempty"`  // "auto" | "never"
	GooglePay string `json:"googlePay,omitempty"` // "auto" | "never"
	Link      string `json:"link,omitempty"`      // "auto" | "never"
}

