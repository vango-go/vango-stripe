# vango-stripe

`vango-stripe` is a Vango integration for Stripe focused on framework-specific concerns:

- Stripe Elements mounted safely inside Vango Islands (DOM ownership boundary)
- Secure Stripe webhook HTTP boundary handling

This package does not wrap Stripe API methods. Applications call `stripe-go` directly in Vango I/O-safe boundaries.

## Documentation Map

- Canonical usage guide: [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md)
- Full detailed spec: [STRIPE_INTEGRATION.md](./STRIPE_INTEGRATION.md)

## Install

```bash
go get github.com/vango-go/vango-stripe@latest
```

## Serve The Island Modules

Vango's island runtime imports islands from `/js/islands/<id>.js` by default.

Required files:

- `/js/islands/stripe-loader.js`
- `/js/islands/stripe-payment-element.js` (island id: `stripe-payment-element`)
- `/js/islands/stripe-express-checkout.js` (island id: `stripe-express-checkout`)

`stripe-payment-element.js` and `stripe-express-checkout.js` import `./stripe-loader.js`, so keep all three files colocated.

### Copy Assets Recipe

```bash
mkdir -p public/js/islands
cp /path/to/vango-stripe/js/islands/stripe-loader.js public/js/islands/
cp /path/to/vango-stripe/js/islands/stripe-payment-element.js public/js/islands/
cp /path/to/vango-stripe/js/islands/stripe-express-checkout.js public/js/islands/
```

### Module Path Override (Fingerprinting / Relocation)

If your app fingerprints island modules, set `data-module` on the same island boundary:

```go
Div(
	JSIsland("stripe-payment-element", p),
	Attr("data-module", "/assets/js/islands/stripe-payment-element.abc123.js"),
)
```

## Elements Flow (Minimal Recipe)

1. Action creates a PaymentIntent via `stripe-go`.
2. Render passes `pi.ClientSecret` + absolute `ReturnURL` into `ui.PaymentElement(...)`.
3. Return page verifies PaymentIntent server-side.
4. Webhook fulfillment is idempotent by Stripe `event.ID`.

```go
createIntent := setup.Action(&s,
	func(ctx context.Context, _ struct{}) (*stripelib.PaymentIntent, error) {
		return routes.GetDeps().Payments.CreatePaymentIntent(ctx, 2999, "usd", map[string]string{"order_id": "ord_123"})
	},
)

return func() *vango.VNode {
	return createIntent.Match(
		vango.OnActionIdle(func() *vango.VNode {
			return Button(OnClick(func() { createIntent.Run(struct{}{}) }), Text("Proceed to payment"))
		}),
		vango.OnActionSuccess(func(pi *stripelib.PaymentIntent) *vango.VNode {
			return routes.GetDeps().StripeUI.PaymentElement(stripe.PaymentElementProps{
				ClientSecret: pi.ClientSecret,
				ReturnURL:    "https://myapp.com/checkout/complete",
			})
		}),
	)
}
```

## Webhooks

Webhook endpoints must:

- verify `Stripe-Signature` against your webhook secret
- verify against raw request body bytes
- be mounted as a standard HTTP handler (CSRF-exempt)
- return 200 for unknown/unhandled events
- return 200 for accepted no-match events (avoid retry storms)
- implement idempotency/dedupe by Stripe `event.ID`

```go
// Mount webhooks on your server mux (off the Vango session loop).
// Do not mount Stripe webhooks as `app.API(...)` routes: Vango API endpoints enforce CSRF by default.
// import "net/http"
mux := http.NewServeMux()
mux.Handle("/webhooks/stripe", stripe.WebhookHandler(
	stripe.WebhookConfig{Secret: os.Getenv("STRIPE_WEBHOOK_SECRET")},
	stripe.On("payment_intent.succeeded", handlers.OnPaymentSucceeded),
	stripe.On("payment_intent.payment_failed", handlers.OnPaymentFailed),
))
mux.Handle("/", app)
app.Server().SetHandler(mux)
```

## Stripe CLI Local Development

```bash
stripe listen --forward-to localhost:8080/webhooks/stripe
stripe trigger payment_intent.succeeded
```

## CSP Requirements

Baseline:

- `script-src https://js.stripe.com`
- `frame-src https://js.stripe.com https://hooks.stripe.com`
- `connect-src https://api.stripe.com`

Optional expansions (only if violations require):

- `connect-src https://q.stripe.com https://errors.stripe.com`
- broader fallback for specific methods only: `https://*.stripe.com`

Strict nonce/hash mode:

1. Preload Stripe.js in `<head>` with a valid nonce, or
2. Render `<meta name="csp-nonce" content="...">` and use `stripe-loader.js` nonce support.

## Operational Guardrails

- respond quickly from webhook handlers; enqueue long-running work
- keep handlers idempotent (`event.ID` dedupe)
- treat Stripe error strings as potentially sensitive
- never log `sk_*` or `whsec_*`
- do not assume webhook ordering

## Manual Smoke Harness (Phase 2)

For browser-level island lifecycle checks (mount/update/destroy + event envelopes):

- open `js/manual/index.html`
- checklist: `js/manual/README.md`

Example:

```bash
cd vango-stripe
python3 -m http.server 8080
# open http://localhost:8080/js/manual/index.html
```

## Common Mistakes

- Calling Stripe APIs from render/setup/session-loop callbacks
- Treating island success as authoritative fulfillment
- Do not mix Elements and Checkout Sessions
- Passing Checkout Session `cs_*` secrets to Elements islands
- Missing CSP `frame-src https://hooks.stripe.com`
- Returning non-2xx for accepted not-found webhook events
