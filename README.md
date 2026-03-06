# vango-stripe

`vango-stripe` is a Vango integration for Stripe focused on framework-specific concerns:

- Stripe Elements mounted safely inside Vango Islands (DOM ownership boundary)
- Secure Stripe webhook HTTP boundary handling

This package does not wrap Stripe API methods. Applications call `stripe-go` directly in Vango I/O-safe boundaries.

## Pricing Boundary

Stripe IDs are infrastructure identifiers, not trusted client inputs.

- For fixed plans or tiers, client code should send a business-level `PlanKey` and the server should resolve it through a server-owned pricing catalog.
- For carts, quotes, or negotiated checkouts, client code should send an opaque server-issued reference such as `checkoutID`; the server must recompute authoritative line items and totals.
- Do not accept raw Stripe IDs such as `price_*`, `prod_*`, `pm_*`, `cus_*`, or `sub_*` from the client as billing decisions.

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
2. Action persists an opaque server-owned return reference bound to the expected PaymentIntent.
3. Render passes `pi.ClientSecret` + absolute `ReturnURL` containing `?ref=...` into `ui.PaymentElement(...)`.
4. Return page verifies by `ref`, not by a URL-supplied Stripe ID.
5. Webhook fulfillment is idempotent by Stripe `event.ID`.

```go
createIntent := setup.Action(&s,
	func(ctx context.Context, _ struct{}) (*payments.PaymentIntentSession, error) {
		return routes.GetDeps().Payments.CreatePaymentIntent(ctx, payments.PaymentIntentParams{
			Amount:      2999,
			Currency:    "usd",
			Description: "Order #ord_123",
			Metadata:    map[string]string{"order_id": "ord_123"},
			OwnerKey:    currentOwnerKey(ctx),
		})
	},
)

return func() *vango.VNode {
	return createIntent.Match(
		vango.OnActionIdle(func() *vango.VNode {
			return Button(OnClick(func() { createIntent.Run(struct{}{}) }), Text("Proceed to payment"))
		}),
		vango.OnActionSuccess(func(session *payments.PaymentIntentSession) *vango.VNode {
			return routes.GetDeps().StripeUI.PaymentElement(stripe.PaymentElementProps{
				ClientSecret: session.PaymentIntent.ClientSecret,
				ReturnURL:    "https://myapp.com/checkout/complete?ref=" + url.QueryEscape(session.ReturnRef),
			})
		}),
	)
}
```

## Return URL Boundary

Treat return URL query params as untrusted transport data.

- Use an opaque server-issued `ref` as the canonical lookup key.
- Do not call Stripe with a `payment_intent=pi_...` read directly from the browser URL.
- If Stripe appends `payment_intent` or `payment_intent_client_secret`, server-side scrub them from the visible URL and keep only `ref`.
- Bind the stored return ref to the current owner or tenant when the flow is authenticated.

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
- Accepting raw Stripe `price_*` or other Stripe IDs from client input
- Passing Checkout Session `cs_*` secrets to Elements islands
- Missing CSP `frame-src https://hooks.stripe.com`
- Returning non-2xx for accepted not-found webhook events
