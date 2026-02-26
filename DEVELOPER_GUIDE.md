# Vango Stripe Developer Guide

## Canonical Status

This file is the canonical developer-facing usage guide for `vango-stripe`.

`STRIPE_INTEGRATION.md` remains the full engineering/spec reference.

Sync workflow:

- Any behavior or API contract change must update both `DEVELOPER_GUIDE.md` and `STRIPE_INTEGRATION.md` in the same PR.
- If the two files diverge, resolve the mismatch before merge.

## Quick Decision Tree

1. Need embedded checkout inside your app UI?
Use Elements flow: PaymentIntent + `ui.PaymentElement(...)` / `ui.ExpressCheckoutElement(...)`.

2. Need fastest hosted checkout path with no islands?
Use Checkout Sessions flow and redirect to Stripe-hosted checkout.

3. Need authoritative fulfillment signal?
Use verified webhook events and/or Stripe API verification on return URL.

4. Do not mix Elements and Checkout Sessions:
Do not create a Checkout Session (`cs_*`) and pass that secret to Elements islands.

## Serve The Island Modules (Required for Elements)

Vango’s island runtime imports islands from `/js/islands/<id>.js` by default. For the Elements flow
(`ui.PaymentElement(...)` / `ui.ExpressCheckoutElement(...)`), your app must serve:

- `/js/islands/stripe-loader.js`
- `/js/islands/stripe-payment-element.js` (island id: `stripe-payment-element`)
- `/js/islands/stripe-express-checkout.js` (island id: `stripe-express-checkout`)

`stripe-payment-element.js` and `stripe-express-checkout.js` import `./stripe-loader.js`, so keep all
three files colocated.

Copy recipe (from your module cache into your app’s `public/`):

```bash
mkdir -p public/js/islands
moddir="$(go list -m -f '{{.Dir}}' github.com/vango-go/vango-stripe)"
cp "$moddir/js/islands/stripe-loader.js" public/js/islands/
cp "$moddir/js/islands/stripe-payment-element.js" public/js/islands/
cp "$moddir/js/islands/stripe-express-checkout.js" public/js/islands/
```

Module path override (fingerprinting/relocation): set `data-module` on the same element as `JSIsland(...)`.

```go
Div(
	JSIsland("stripe-payment-element", p),
	Attr("data-module", "/assets/js/islands/stripe-payment-element.abc123.js"),
)
```

## 37.9 Payments (Stripe)

### 37.9.1 Two Integration Models

**Elements (PaymentIntent + Elements islands)** — canonical path for `vango-stripe`:

- Server creates a PaymentIntent via `stripe-go` in an Action
- Render passes the PaymentIntent `ClientSecret` into `ui.PaymentElement(...)` and/or `ui.ExpressCheckoutElement(...)`
- User completes payment inside your app UI
- Server verifies via API and/or webhooks before fulfilling

**Checkout Sessions (redirect)** — simpler alternative with no islands:

- Server creates a Checkout Session in an Action
- Post-success Effect navigates to `session.URL`
- User completes payment on Stripe’s hosted page
- Server fulfills from webhooks (`checkout.session.completed`)

Do not mix these models. Do not pass Checkout Session `cs_*` secrets into Elements islands.

### 37.9.2 The Access Rule (MUST)

All Stripe API calls that are part of a Vango session-driven UI must occur in:

- Resource loaders
- Action work functions
- Standalone HTTP handlers that run off the session loop (for example, webhooks)

Do not call Stripe APIs from setup callbacks, render closures, island message handlers, or lifecycle callbacks.

### 37.9.3 Environment Configuration

```bash
STRIPE_SECRET_KEY="sk_test_..."
STRIPE_PUBLISHABLE_KEY="pk_test_..."
STRIPE_WEBHOOK_SECRET="whsec_..."
```

- `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET` are secrets.
- `STRIPE_PUBLISHABLE_KEY` is safe for client exposure.

### 37.9.4 Dependency Injection Pattern

```go
package payments

import (
	"context"

	stripelib "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/client"
)

type Service interface {
	CreatePaymentIntent(ctx context.Context, amount int64, currency string, metadata map[string]string) (*stripelib.PaymentIntent, error)
	GetPaymentIntent(ctx context.Context, id string) (*stripelib.PaymentIntent, error)
}

type StripeService struct {
	client *client.API
}

func New(secretKey string) *StripeService {
	c := &client.API{}
	c.Init(secretKey, nil)
	return &StripeService{client: c}
}

func (s *StripeService) CreatePaymentIntent(ctx context.Context, amount int64, currency string, metadata map[string]string) (*stripelib.PaymentIntent, error) {
	params := &stripelib.PaymentIntentParams{
		Amount:   stripelib.Int64(amount),
		Currency: stripelib.String(currency),
		AutomaticPaymentMethods: &stripelib.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripelib.Bool(true),
		},
	}
	for k, v := range metadata {
		params.AddMetadata(k, v)
	}
	params.Context = ctx
	return s.client.PaymentIntents.New(params)
}

func (s *StripeService) GetPaymentIntent(ctx context.Context, id string) (*stripelib.PaymentIntent, error) {
	params := &stripelib.PaymentIntentParams{}
	params.Context = ctx
	return s.client.PaymentIntents.Get(id, params)
}
```

App wiring:

```go
paymentsSvc := payments.New(os.Getenv("STRIPE_SECRET_KEY"))
stripeUI := stripe.MustNewUI(stripe.UIConfig{
	PublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
	Locale:         "auto",
})
```

Notes:

- `stripe.UIConfig` is client-facing; it MUST NOT contain secret material. Only pass `pk_*` publishable keys.
- Prefer constructing `*stripe.UI` once at app init and passing it via DI.

### 37.9.5 Elements Flow Recipe

1. Action creates PaymentIntent via `stripe-go`.
2. Render passes `pi.ClientSecret` + absolute `ReturnURL` into `ui.PaymentElement(...)`.
3. Return page verifies PaymentIntent server-side.
4. Webhook handles final fulfillment idempotently.

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

### 37.9.6 Express Checkout + Payment Element Pattern

Use Express Checkout first for wallets, then show Payment Element fallback for card entry.
Treat island messages as untrusted hints only.

```go
routes.GetDeps().StripeUI.ExpressCheckoutElement(stripe.ExpressCheckoutProps{
	ClientSecret: pi.ClientSecret,
	ReturnURL:    "https://myapp.com/checkout/complete",
},
	OnIslandMessage(func(msg vango.IslandMessage) {
		// Decode and update local UI state only.
		// Fulfillment still happens from API/webhooks.
	}),
)

routes.GetDeps().StripeUI.PaymentElement(stripe.PaymentElementProps{
	ClientSecret: pi.ClientSecret,
	ReturnURL:    "https://myapp.com/checkout/complete",
})
```

### 37.9.7 Return URL Verification Pattern

Read `payment_intent` from the return URL and verify through a server-side Resource/API call.

```go
status := setup.ResourceKeyed(&s,
	func() string { return props.Get().PaymentIntentID },
	func(ctx context.Context, id string) (*stripelib.PaymentIntent, error) {
		return routes.GetDeps().Payments.GetPaymentIntent(ctx, id)
	},
)
```

## 37.10 Webhook Handling (Stripe)

### 37.10.1 Why Webhooks Are Required

Many payment methods complete asynchronously. Webhooks are required for reliable fulfillment.

Never fulfill from client-side island success alone.

### 37.10.2 Webhook Route Registration

Stripe webhooks are a standard HTTP boundary (off the Vango session loop). Mount the webhook handler
on your server’s HTTP mux so it is **CSRF-exempt** and receives the raw request body bytes for signature verification.

Do not mount Stripe webhooks as `app.API(...)` routes: Vango’s API endpoints enforce CSRF by default.

```go
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

Recommended hardening: set `WebhookConfig.ExpectedLivemode` per environment to prevent test-mode events reaching live endpoints (and vice versa).

### 37.10.3 Response Code Discipline

| Scenario | Response | Stripe Behavior |
|---|---|---|
| Event accepted and processed | 200 | No retry |
| Event accepted, no local match | 200 | No retry |
| Transient infra failure | 500 | Retries with backoff |
| Maintenance window | 503 (`HandlerError`) | Retries with backoff |
| Invalid signature | 400 | No retry |

Return 200 for accepted not-found events to avoid retry storms.

### 37.10.4 Idempotency Guidance

Deduplicate by Stripe `event.ID` before running fulfillment side effects.

```go
result, err := db.Exec(ctx.Request.Context(),
	`INSERT INTO processed_events (event_id, event_type, processed_at)
	 VALUES ($1, $2, now())
	 ON CONFLICT (event_id) DO NOTHING`,
	ctx.Event.ID, string(ctx.Event.Type),
)
if err != nil {
	return err
}
if result.RowsAffected() == 0 {
	return nil // already processed
}
```

## 37.11 Testing Payments

### 37.11.1 Service Layer Unit Testing

Mock your app-level `payments.Service` interface in component/action tests.
Do not mock `vango-stripe` itself.

### 37.11.2 Webhook Handler Testing

Test signature verification and HTTP boundary behavior using raw payload bytes.

Signature helper:

```go
func stripeSignatureHeader(t time.Time, secret string, payload []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	signed := ts + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signed))
	sig := hex.EncodeToString(mac.Sum(nil))
	return "t=" + ts + ",v1=" + sig
}
```

### 37.11.3 Stripe CLI Local Development

```bash
stripe listen --forward-to localhost:8080/webhooks/stripe
stripe trigger payment_intent.succeeded
```

### 37.11.4 Integration Testing

Use Stripe test keys and gate integration tests with a build tag such as `//go:build integration`.

## Appendix H Operational Notes

### H.1 CSP Requirements

Baseline directives:

- `script-src https://js.stripe.com`
- `frame-src https://js.stripe.com https://hooks.stripe.com`
- `connect-src https://api.stripe.com`

### H.2 Optional CSP Expansion

For some payment methods or telemetry endpoints, add only what violations require:

- `connect-src https://q.stripe.com https://errors.stripe.com`
- broader fallback only when required: `https://*.stripe.com`

Never widen `script-src` to wildcard.

### H.3 Nonce/Hash CSP Strategy

If your policy is nonce/hash based and does not allow `https://js.stripe.com` directly:

1. Preload Stripe.js in the document head with a valid nonce, or
2. Render `<meta name="csp-nonce" content="...">` and use `stripe-loader.js` nonce support.

### H.4 Operational Guardrails

| Requirement | Guidance |
|---|---|
| HTTPS | Required in production |
| Webhook availability | Public endpoint reachable by Stripe |
| Response latency | Respond quickly; enqueue long work |
| Idempotency | Deduplicate by `event.ID` |
| CSRF | Webhook endpoints must be CSRF-exempt; mount on the server mux (not `app.API(...)`) |
| Ordering | Do not assume ordered event delivery |

### H.5 Troubleshooting

| Scenario | Symptom | Resolution |
|---|---|---|
| Missing CSP script-src | Blank Stripe UI | Add `https://js.stripe.com` |
| Missing frame-src hooks domain | 3DS modal blocked | Add `https://hooks.stripe.com` |
| CSRF enforced on webhook | 403 webhook responses | Mount webhook on server mux (not `app.API(...)`) |
| Wrong secret/body consumed | 400 invalid signature | Verify secret and raw-body handling |
| Retry storm | repeated webhook retries | Return 200 for accepted no-match events |
| Wrong secret type in Element | "No such PaymentIntent" | Use `pi_*_secret_*`, not `cs_*` |

## Do Not Do This (Footguns)

- Do not call Stripe APIs from render/setup/session-loop callbacks.
- Do not fulfill orders based on island `confirm-result` success.
- Do not pass Checkout Session `cs_*` secrets into Elements islands.
- Do not omit CSP `frame-src` for `https://hooks.stripe.com`.
- Do not return non-2xx for accepted not-found webhook events.
