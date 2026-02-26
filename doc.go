// Package stripe provides Vango-specific Stripe integration primitives.
//
// Scope boundary:
//
// This package solves Vango integration concerns (islands + webhook HTTP boundary).
// Applications call stripe-go directly for Stripe API operations.
//
// Design invariants (must not regress):
//
//  1. I/O boundary: Stripe API calls must only happen in Vango I/O-safe boundaries
//     (Resource loaders, Action work functions, or standalone HTTP handlers like webhooks).
//     Never call Stripe from render/setup/event handlers running on the session loop.
//
//  2. DOM ownership: Stripe Elements must be mounted inside Islands. Stripe mutates DOM
//     and embeds iframes; Vango must not patch inside that subtree.
//
//  3. Server-authoritative: client-side island events are hints only. Payment state must
//     be verified server-side via the Stripe API and/or verified webhooks before fulfilling.
//
//  4. Secrets: never expose Stripe secret keys (sk_*) or webhook secrets (whsec_*) in
//     logs, errors, or client props.
//
// Documentation map:
//
//   - Canonical usage guide: DEVELOPER_GUIDE.md
//   - Full detailed spec/reference: STRIPE_INTEGRATION.md
//
// Quick wiring:
//
// ui := stripe.MustNewUI(stripe.UIConfig{
// 	PublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"), // pk_*
// })
//
// // In a render closure, pass a PaymentIntent client secret (pi_*_secret_*) created
// // in an Action, plus an absolute return URL:
// _ = ui.PaymentElement(stripe.PaymentElementProps{
// 	ClientSecret: piClientSecret,
// 	ReturnURL:    "https://example.com/checkout/complete",
// })
//
// // Webhook boundary (CSRF-exempt HTTP handler):
// //
// // Mount on your server mux (off the Vango session loop). Do not mount Stripe webhooks
// // as `app.API(...)` routes: Vango API endpoints enforce CSRF by default.
// mux := http.NewServeMux()
// mux.Handle("/webhooks/stripe", stripe.WebhookHandler(
// 	stripe.WebhookConfig{Secret: os.Getenv("STRIPE_WEBHOOK_SECRET")},
// 	stripe.On("payment_intent.succeeded", handlers.OnPaymentSucceeded),
// ))
// mux.Handle("/", app)
// app.Server().SetHandler(mux)
//
// Static assets:
//
// Host applications must serve the ES modules from /js/islands/:
//
//   - /js/islands/stripe-loader.js
//   - /js/islands/stripe-payment-element.js
//   - /js/islands/stripe-express-checkout.js
package stripe
