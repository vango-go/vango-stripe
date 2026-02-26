# Vango + Stripe: Integration Specification

**Status:** Draft (spec)
**Date:** 2026-02-24
**Last Reviewed:** 2026-02-25

---

## Document Structure

1. **Design Invariants** — Non-negotiable acceptance criteria.
2. **The `vango-stripe` Package Specification** — Islands (Go + JS), Webhook Handler, Helpers.
3. **Developer Guide Sections** — §37.9 (Payments with Stripe), §37.10 (Webhook Handling), §37.11 (Testing Payments).
4. **Appendix H** — CSP Requirements, Checkout Sessions Recipe, Operational Guardrails, Troubleshooting.

All server-side Stripe API calls use `stripe-go` (v84+) directly. `vango-stripe` does **not** wrap,
proxy, or re-export any `stripe-go` API methods. The package exists solely to solve
Vango-specific integration problems: DOM ownership for Stripe Elements, webhook HTTP boundary
handling, and framework-aware security configuration.

---

## Scope Boundary (Explicit)

### What `vango-stripe` provides

1. **Two Island helpers** (`(*stripe.UI).PaymentElement`, `(*stripe.UI).ExpressCheckoutElement`) with
   bundled client JS that safely mount Stripe Elements inside Vango's VDOM without causing
   patch mismatches.
2. **A thin Webhook handler** (`stripe.WebhookHandler`) that skips CSRF, buffers raw bytes,
   verifies Stripe signatures, and dispatches to typed Go handler functions.
3. **Documentation and recipes** for using `stripe-go` directly inside Vango Resources/Actions,
   dependency injection patterns, and testing strategies.
4. **A small UI helper type** (`stripe.UI`) that applies default (client-safe) configuration
   and renders islands with SSR placeholders.

### What `vango-stripe` does NOT provide

- API method wrappers (use `stripe-go` directly)
- A `Client` interface or `TestClient` mock (app-level concern; recipe in guide)
- Safe error wrapping or logging guarantees (treat Stripe errors as potentially sensitive; log carefully)
- Config/key validation beyond basic emptiness and obvious prefix/shape checks
- CSP mutation helpers (documented; use Vango's existing config)
- Stripe.js loading or initialization outside island boundaries
- A Vango static-file mounting mechanism (apps must serve the island modules under `/js/islands/*`)

### Canonical Integration Model

`vango-stripe` is **Elements-first**: the islands mount Stripe's Payment Element and
Express Checkout Element, which require a **PaymentIntent** client secret. This is the
primary documented flow.

**v1 scope note:** SetupIntents are not covered by this spec. If/when we add SetupIntent
support, it must use `stripe.confirmSetup(...)` and have an explicit intent-kind contract.

**Stripe Checkout Sessions** (redirect to Stripe-hosted checkout page) are a valid
alternative that does not use islands at all. This flow is documented as a separate
recipe in Appendix H.5.

These two models must not be mixed: do not create a Checkout Session and pass its
client secret to a Payment Element island. They are different Stripe products with
different client secret semantics.

---

## Design Invariants (Non-Negotiable)

### I1. Stripe API Call Boundary Invariant (Vango Contract)

All Stripe API calls (via `stripe-go`) MUST occur only inside **off-session-loop I/O boundaries**:

- **Resource loaders**
- **Action work functions**
- **Standalone HTTP handlers** that are not executed on the Vango session loop (for example: Stripe webhooks, typed API routes, background jobs)

Stripe API calls MUST NOT occur from:

- Setup callbacks
- Render closures
- Event handlers (except dispatching to Actions)
- Lifecycle callbacks (`OnMount`, `Effect`, `OnChange`)
- Any goroutine spawned from component code

### I2. DOM Ownership Invariant (Island Contract)

Stripe Elements mount iframes and manage DOM subtrees that Vango's VDOM patcher MUST NOT
touch. All Stripe UI components MUST be implemented as Vango Islands (§22), not as
server-rendered elements or hooks.

### I3. Server-Authoritative Payment State Invariant

Payment state (succeeded, failed, pending, disputed) MUST be determined server-side via:

- Stripe API calls (in Resources/Actions), or
- Verified webhook events (via `stripe.WebhookHandler`)

Client-side island events (e.g., `confirmPayment` success callback) are **hints only** and
MUST NOT be treated as authoritative payment confirmation.

### I4. Webhook Security Invariant

Webhook endpoints MUST:

- Verify the `Stripe-Signature` header using the webhook endpoint secret
- Use the raw, unparsed request body bytes for signature verification
- Be exempt from Vango's CSRF middleware (Stripe cannot provide CSRF tokens)
- Reject events with invalid or missing signatures (HTTP 400)
- Return HTTP 200 promptly to avoid Stripe retry storms

### I5. Secrets Invariant

`vango-stripe` MUST NOT log, format, return in errors, or expose in client-sent props:

- Stripe secret API keys (`sk_live_*`, `sk_test_*`)
- Webhook endpoint secrets (`whsec_*`)

Publishable keys (`pk_live_*`, `pk_test_*`) are designed for client exposure.

Client secrets (from a PaymentIntent) are designed for client exposure, scoped to a
single payment flow. They SHOULD be generated per-user/per-order and not persisted.

**Error logging posture (MUST):** treat Stripe SDK errors as potentially sensitive.
In production, log only whitelisted structured fields (for example: Stripe error
`type`, `code`, `decline_code`, and `request_id`) and avoid logging raw error strings
that may include user data or request parameters.

### I6. Render Purity Invariant (Vango Contract)

Island helper methods (`ui.PaymentElement(...)`, `ui.ExpressCheckoutElement(...)`) are called
from render closures and MUST be **render-pure**:

- No I/O, no randomness, no time-dependent computation
- No signal writes (`Signal.Set(...)`)
- No side effects (`Navigate(...)`, goroutines)
- Deterministic output for the same inputs

### I7. User-Gesture Confirmation Invariant (Stripe UX/Security Posture)

User-confirmation flows (for example, `stripe.confirmPayment(...)`) SHOULD be initiated directly
from a user gesture handled on the client (e.g., a click inside the island) to avoid browser
gesture-gating issues in wallet/redirect flows.

Accordingly, `vango-stripe` islands:

- MAY mount elements without confirming (mount-only mode), but
- MUST NOT attempt to “confirm later” purely from a server → island message that is not
  directly triggered by a client-side user gesture.
---

# Part 1: The `vango-stripe` Package Specification

**Import Path:** `github.com/vango-go/vango-stripe`

**Dependencies:**

- `github.com/stripe/stripe-go/v84` (peer dependency — developer installs directly)
- `github.com/vango-go/vango` (for island helpers, HTTP handler types)

---

## 1.1 Configuration

```go
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
	// Default: "auto"
	Locale string

	// Appearance is the default Stripe Elements appearance configuration.
	// Default: nil (Stripe default theme)
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
		return nil, fmt.Errorf("stripe: UIConfig.PublishableKey must be a publishable key (pk_*), got %q", cfg.PublishableKey)
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
```

**Recommended:** set `WebhookConfig.ExpectedLivemode` in each environment to prevent misrouted
test-mode events hitting live handlers (and vice versa). Example:

```go
livemode := false // test endpoint
cfg := stripe.WebhookConfig{
	Secret:           os.Getenv("STRIPE_WEBHOOK_SECRET"),
	ExpectedLivemode: &livemode,
}
```

```go
package stripe

// ElementsAppearance configures the visual appearance of Stripe Elements.
// Maps to Stripe's Appearance API: https://docs.stripe.com/elements/appearance-api
type ElementsAppearance struct {
	Theme     string                       `json:"theme,omitempty"`
	Variables map[string]string            `json:"variables,omitempty"`
	Rules     map[string]map[string]string `json:"rules,omitempty"`
}
```

---

## 1.2 Island Components

### 1.2.1 Design: Why Islands, Not Hooks

Stripe Elements mount iframes and manage DOM subtrees. Per Vango §22, this requires an
Island: Vango renders the container, does not patch inside it, and the island JS owns
everything within the boundary. Hooks would be incorrect because Stripe's structural DOM
mutations would conflict with Vango's patcher.

### 1.2.2 Island Lifecycle Contract (Vango v1 Runtime Contract)

Vango islands are **client-owned opaque DOM subtrees**. The Vango thin client mounts an
island by dynamically importing an ES module and calling its mount function.

**Module entrypoint (REQUIRED):**

* The island module MUST export `mount(el, props, api)` (named export), or export a
  default function with the same signature.
* The thin client calls `mount(...)` **synchronously** and does **not** `await` it.

**Return shape (REQUIRED):** `mount(...)` MUST return synchronously one of:

1. A function `destroy()`; or
2. An object with any of:

   * `update(nextProps)` (optional)
   * `destroy()` (optional)
   * `onMessage(payload)` (optional) — for server → island messages
   * `onReconnect(el, props, api)` (optional) — called after WS reconnect/resync

This is the contract implemented by Vango's current thin client `IslandManager`.

**Async work is allowed, but MUST be cancellation-safe:**

* A Stripe island will typically start async work (load Stripe.js, create Elements).
  Because `mount(...)` is not awaited, the island MUST:
  * catch and report errors (never rely on unhandled promise rejections),
  * tolerate `destroy()` being called before async setup completes, and
  * avoid mutating the DOM after it has been destroyed/unmounted.

**`update(...)` guidance (SHOULD):**

* `update(nextProps)` SHOULD be fast and idempotent.
* For Stripe Elements, many prop changes require a full remount; `update` MAY choose
  to destroy and re-mount internally when a remount key changes.

### 1.2.2.1 Remount Semantics (Recommended v1 Posture)

Vango's thin client calls an island instance's `update(nextProps)` only when the island
element's raw `data-props` attribute string changes. If `data-props` is identical, Vango
skips calling `update(...)` entirely.

`vango-stripe` adds a **second layer**: a `remountKey(props)` function in the shared
Stripe loader module. The island `update(...)` compares `remountKey(nextProps)` to the
previous key and remounts only when the key changes.

**Vango-wide default (recommended):** rely on

1. raw `data-props` string equality (client gate), and
2. `remountKey(props)` (island gate)

without introducing a server-computed remount hash.

**Advanced escape hatch (MAY): server-provided remount keys**

If you have very large props, custom JSON encoders, or you want the server to define
exactly which changes require a remount, you MAY include a stable string field in the
island props (recommended name: `remountKey`) and have `remountKey(props)` return it.

This avoids any dependence on JavaScript object key iteration order and keeps the remount
decision fully server-defined.

**`destroy()` requirements (MUST):**

* MUST be synchronous and idempotent.
* MUST remove event listeners and unmount Stripe Elements.
* MUST not throw (catch defensively).

### 1.2.3 `(*stripe.UI).PaymentElement` (Server-Side Go)

```go
	package stripe
	
	import (
		"net/url"
		"strings"

		"github.com/vango-go/vango"
		. "github.com/vango-go/vango/el"
	)

	// PaymentElementProps configures the Payment Element island.
	//
	// This component is part of the Elements integration model. It requires a
	// client secret from a PaymentIntent (NOT a Checkout Session).
	type PaymentElementProps struct {
		// ClientSecret is the client secret from a PaymentIntent.
		// REQUIRED. Create a PaymentIntent in an Action, then pass its ClientSecret.
		//
		// NOTE: This is NOT a Checkout Session client secret.
		ClientSecret string `json:"clientSecret"`

		// ReturnURL is the URL Stripe redirects to after payment confirmation
		// (for redirect-based methods like 3D Secure, bank redirects).
		// REQUIRED. Must be an absolute URL.
		ReturnURL string `json:"returnURL"`

		// PublishableKey overrides the Stripe publishable key for this element.
		// If empty, uses UIConfig.PublishableKey from the *UI instance.
		PublishableKey string `json:"publishableKey,omitempty"`

		// RemountKey optionally overrides the island remount decision.
		// If set, the client-side `remountKey(props)` returns this value directly.
		//
		// This is an advanced escape hatch. Most applications should omit it and
		// rely on Vango's `data-props` equality gate plus the default JS remount key.
		RemountKey string `json:"remountKey,omitempty"`

		// Layout controls the Payment Element layout.
		// One of: "tabs", "accordion", "auto". Default: "auto"
		Layout string `json:"layout,omitempty"`

		// Locale overrides the locale. If empty, uses UIConfig.Locale or "auto".
		Locale string `json:"locale,omitempty"`

		// Appearance overrides the appearance. If nil, uses UIConfig.Appearance.
		Appearance *ElementsAppearance `json:"appearance,omitempty"`

		// Business is optional business information shown in the Element.
		Business *ElementsBusiness `json:"business,omitempty"`

		// EmitChangeEvents controls whether the island emits high-frequency "change" events.
		// Default: false.
		//
		// Rationale: Stripe Element "change" can be noisy. Most apps only need:
		// - "ready"
		// - "confirm-started"
		// - "confirm-result"
		// - "error"
		EmitChangeEvents bool `json:"emitChangeEvents,omitempty"`

		// DisableSubmitButton disables the built-in confirm button.
		//
		// IMPORTANT (I7): When true, the island will NOT call stripe.confirmPayment() at all.
		// This is a mount-only mode intended for advanced integrations that implement their own
		// client-side confirmation flow.
		//
		// Default: false.
		DisableSubmitButton bool `json:"disableSubmitButton,omitempty"`

		// SubmitButtonText customizes the submit button label. Default: "Pay now"
		SubmitButtonText string `json:"submitButtonText,omitempty"`

		// ID is an optional DOM ID for the island container.
		// If empty, no ID attribute is set (the island JS mounts into `el` by
		// reference and does not require a DOM ID).
		//
		// Set this only if you need to reference the container from external CSS
		// or JavaScript. If you render multiple PaymentElements on one page,
		// each MUST have a unique ID (or omit ID entirely).
		ID string `json:"-"`
	}

// ElementsBusiness contains business information displayed in the Element.
type ElementsBusiness struct {
	Name string `json:"name,omitempty"`
}

	// PaymentElement renders a Vango Island that mounts the Stripe Payment Element.
//
// This function is render-pure (I6): it produces deterministic VNode output
// for the same inputs and performs no side effects.
//
// The island:
//   - Loads Stripe.js (if not already loaded)
//   - Creates a Stripe Elements instance with the PaymentIntent client secret
//   - Mounts the Payment Element into the island boundary
//   - Handles stripe.confirmPayment() on submit (unless DisableSubmitButton is true)
//   - Emits server events: "ready", "confirm-started", "confirm-result"
//   - Optionally emits "change" when EmitChangeEvents is true (throttled client-side)
//
// The server MUST verify payment status via the Stripe API or webhooks
// before fulfilling orders (I3).
	func (ui *UI) PaymentElement(p PaymentElementProps, attrs ...vango.Attr) *vango.VNode {
		if ui == nil {
			panic("stripe.UI.PaymentElement: nil UI")
		}
		if p.ClientSecret == "" {
			panic("stripe.UI.PaymentElement: ClientSecret is required")
		}
		// Defensive: prevent mixing Stripe Checkout Sessions with Elements.
		if strings.HasPrefix(p.ClientSecret, "cs_") {
			panic("stripe.UI.PaymentElement: ClientSecret looks like a Checkout Session (cs_*). Use a PaymentIntent client secret.")
		}
		if !strings.HasPrefix(p.ClientSecret, "pi_") || !strings.Contains(p.ClientSecret, "_secret_") {
			panic("stripe.UI.PaymentElement: ClientSecret must be a PaymentIntent client secret (pi_*_secret_*). SetupIntents and other intent kinds are not supported in v1.")
		}
		if p.ReturnURL == "" {
			panic("stripe.UI.PaymentElement: ReturnURL is required")
		}
		if u, err := url.Parse(p.ReturnURL); err != nil || !u.IsAbs() {
			panic("stripe.UI.PaymentElement: ReturnURL must be an absolute URL")
		}

		// Apply defaults from UI config (pure reads only)
		if p.PublishableKey == "" {
			p.PublishableKey = ui.cfg.PublishableKey
		}
		if p.PublishableKey == "" {
			panic("stripe.UI.PaymentElement: PublishableKey is required (prop or UI config)")
		}
		if !strings.HasPrefix(p.PublishableKey, "pk_") {
			panic("stripe.UI.PaymentElement: PublishableKey must be a publishable key (pk_*), not a secret or other key type")
		}
		if p.Layout != "" && p.Layout != "auto" && p.Layout != "tabs" && p.Layout != "accordion" {
			panic("stripe.UI.PaymentElement: invalid Layout (expected auto|tabs|accordion)")
		}
		if p.Locale == "" {
			p.Locale = ui.cfg.locale()
		}
		if p.Appearance == nil && ui.cfg.Appearance != nil {
			p.Appearance = ui.cfg.Appearance
		}

		base := []any{
			Class("vango-stripe-payment-element"),
			JSIsland("stripe-payment-element", p),
			// SSR-only placeholder (opaque subtree; Vango will not patch inside after mount).
			IslandPlaceholder(Text("Loading payment…")),
		}
		if p.ID != "" {
			base = append([]any{ID(p.ID)}, base...)
		}
		for _, a := range attrs {
			base = append(base, a)
		}
		return Div(base...)
	}
	```

### 1.2.4 `(*stripe.UI).ExpressCheckoutElement` (Server-Side Go)

```go
	package stripe
	
	import (
		"net/url"
		"strings"

		"github.com/vango-go/vango"
		. "github.com/vango-go/vango/el"
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
		// If set, the client-side `remountKey(props)` returns this value directly.
		//
		// This is an advanced escape hatch. Most applications should omit it and
		// rely on Vango's `data-props` equality gate plus the default JS remount key.
		RemountKey string `json:"remountKey,omitempty"`

		// Locale override. If empty, uses UIConfig.Locale or "auto".
		Locale string `json:"locale,omitempty"`

		// Appearance override. If nil, uses UIConfig.Appearance.
		Appearance *ElementsAppearance `json:"appearance,omitempty"`

		// ButtonType: "buy", "pay", "book", "donate", "checkout", "subscribe", "plain".
		// Default: "buy"
		ButtonType string `json:"buttonType,omitempty"`

		// ButtonTheme: "dark", "light", "outline". Default: Stripe auto-selects.
		ButtonTheme string `json:"buttonTheme,omitempty"`

		// ButtonHeight in pixels. Range: 40–55. Default: 44.
		ButtonHeight int `json:"buttonHeight,omitempty"`

		// Wallets controls which wallets are displayed. If nil, all shown.
		Wallets *ExpressCheckoutWallets `json:"wallets,omitempty"`

		// ID is an optional DOM ID. If empty, no ID is set.
		ID string `json:"-"`
	}

// ExpressCheckoutWallets controls wallet button visibility.
type ExpressCheckoutWallets struct {
	ApplePay  string `json:"applePay,omitempty"`  // "auto" | "never"
	GooglePay string `json:"googlePay,omitempty"` // "auto" | "never"
	Link      string `json:"link,omitempty"`      // "auto" | "never"
}

	// ExpressCheckoutElement renders a Vango Island that mounts the Stripe
// Express Checkout Element (Apple Pay, Google Pay, Link).
//
// This function is render-pure (I6).
//
	// If no wallets are available, the island emits "no-wallets".
	// Applications can then hide the wallet UI server-side (e.g., local signal toggled by OnIslandMessage).
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
			panic("stripe.UI.ExpressCheckoutElement: ButtonHeight out of range (40–55)")
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
			Class("vango-stripe-express-checkout"),
			JSIsland("stripe-express-checkout", p),
			IslandPlaceholder(Text("Loading wallets…")),
		}
		if p.ID != "" {
			base = append([]any{ID(p.ID)}, base...)
		}
		for _, a := range attrs {
			base = append(base, a)
		}
		return Div(base...)
	}
	```

### 1.2.5 Shared Stripe.js Loader (`stripe-loader.js`)

Both island modules import this shared module to ensure a single Stripe.js script
tag and a single cache of Stripe instances per publishable key.

**Nonce CSP note (REQUIRED for some CSPs):** If your CSP uses nonce- or hash-based `script-src`
without a host allowlist for `https://js.stripe.com`, dynamic script injection can be blocked.
To support this, either:

1. Preload Stripe.js in your layout with the correct nonce, **or**
2. Render `<meta name="csp-nonce" content="...">` and let `stripe-loader.js` apply `script.nonce`.

```javascript
// stripe-loader.js
//
// Shared Stripe.js loader for vango-stripe islands.
// Caches Stripe instances per publishable key. Ensures Stripe.js is loaded
// only once regardless of how many islands are mounted.

let stripeJsPromise = null;
const stripeInstances = new Map();

function loadStripeJs() {
	  if (window.Stripe) {
	    return Promise.resolve();
	  }
	  if (stripeJsPromise) {
	    return stripeJsPromise;
	  }

	  stripeJsPromise = new Promise((resolve, reject) => {
	    const existing = document.querySelector(
	      'script[src="https://js.stripe.com/v3/"]'
	    );
	    if (existing) {
	      if (existing.dataset.loaded === "true") {
	        resolve();
	        return;
	      }
	      existing.addEventListener("load", () => resolve(), { once: true });
	      existing.addEventListener("error", () => {
	        stripeJsPromise = null;
	        reject(new Error("Failed to load Stripe.js"));
	      }, { once: true });
	      return;
	    }

	    const script = document.createElement("script");
	    script.src = "https://js.stripe.com/v3/";
	    script.async = true;
	    const nonceMeta = document.querySelector('meta[name="csp-nonce"]');
	    if (nonceMeta && nonceMeta.getAttribute("content")) {
	      script.nonce = nonceMeta.getAttribute("content");
	    }
	    script.onload = () => {
	      script.dataset.loaded = "true";
	      resolve();
	    };
	    script.onerror = () => {
	      stripeJsPromise = null;
	      reject(new Error("Failed to load Stripe.js"));
	    };
	    document.head.appendChild(script);
	  });

	  return stripeJsPromise;
}

/**
 * Returns a Promise<Stripe> for the given publishable key.
 * Loads Stripe.js on first call; reuses cached instances thereafter.
 */
	export function getStripe(publishableKey) {
	  if (stripeInstances.has(publishableKey)) {
	    return stripeInstances.get(publishableKey);
	  }
	  const promise = loadStripeJs()
	    .then(() => {
	      if (!window.Stripe) {
	        throw new Error("Stripe.js loaded but window.Stripe is missing");
	      }
	      return window.Stripe(publishableKey);
	    })
	    .catch((err) => {
	      // Allow retry after transient failure.
	      stripeInstances.delete(publishableKey);
	      throw err;
	    });

	  stripeInstances.set(publishableKey, promise);
	  return promise;
	}

	/**
	 * Returns a stable key for props that require a full remount when changed.
	 * Used by the island instance's update() to skip no-op remounts.
	 */
	export function remountKey(props) {
	  // Advanced escape hatch: allow a server-provided key to fully control remount semantics.
	  // If present, this MUST be a stable string (for example: a hash of selected fields).
	  if (props && typeof props.remountKey === "string" && props.remountKey.length > 0) {
	    return props.remountKey;
	  }
	  return JSON.stringify({
	    clientSecret: props.clientSecret,
	    publishableKey: props.publishableKey,
	    locale: props.locale,
	    appearance: props.appearance,
	    returnURL: props.returnURL,
	    // Element options that affect behavior/structure should remount.
	    layout: props.layout,
	    business: props.business,
	    emitChangeEvents: props.emitChangeEvents,
	    disableSubmitButton: props.disableSubmitButton,
	    submitButtonText: props.submitButtonText,
	    buttonType: props.buttonType,
	    buttonTheme: props.buttonTheme,
	    buttonHeight: props.buttonHeight,
	    wallets: props.wallets,
	  });
	}
	```

**Server-computed `remountKey` recipe (optional)**

If you opt into a server-provided `remountKey`, compute it from a canonical
representation of only the fields that require a full remount (for example: client secret,
publishable key, locale, appearance, element options).

A simple deterministic recipe in Go:

```go
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func MustStripeIslandRemountKey(v any) string {
	b, err := json.Marshal(v) // encoding/json produces a stable key order for map[string]...
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
```

Then pass it in props:

```go
ui.PaymentElement(stripe.PaymentElementProps{
	ClientSecret: pi.ClientSecret,
	ReturnURL:    "https://myapp.com/checkout/complete",
	RemountKey:   MustStripeIslandRemountKey(struct {
		ClientSecret string
		Locale       string
		Appearance   *stripe.ElementsAppearance
		Layout       string
	}{
		ClientSecret: pi.ClientSecret,
		Locale:       "auto",
		Appearance:   appearance,
		Layout:       "auto",
	}),
})
```

**Important:** `vango-stripe` does not require this; it is an optional hardening and
performance optimization when your application needs server-defined remount behavior.

### 1.2.6 Island Client JS (`stripe-payment-element.js`)

```javascript
// stripe-payment-element.js
//
// Vango Island module for the Stripe Payment Element.

	import { getStripe, remountKey } from "./stripe-loader.js";

	// stripe-payment-element.js
	//
	// Vango Island module for the Stripe Payment Element.
	//
	// Contract: exports mount(el, props, api) and returns {update, destroy}.

	export function mount(el, props, api) {
	  const state = {
	    token: 0,
	    key: "",
	    elements: null,
	    paymentElement: null,
	    confirmButton: null,
	    changeTimer: null,
	    pendingChange: null,
	    lastChangeSentAt: 0,
	    destroyed: false,
	  };

	  function send(payload) {
	    try {
	      api?.send?.(payload);
	    } catch (err) {
	      console.error("[vango-stripe] island send failed:", err);
	    }
	  }

	  function destroy() {
	    if (state.destroyed) return;
	    state.destroyed = true;
	    state.token++;

	    try {
	      if (state.changeTimer) {
	        clearTimeout(state.changeTimer);
	      }
	    } catch {}
	    state.changeTimer = null;
	    state.pendingChange = null;

	    try {
	      if (state.confirmButton) {
	        state.confirmButton.remove();
	      }
	    } catch {}
	    state.confirmButton = null;

	    try {
	      state.paymentElement?.unmount?.();
	    } catch {}
	    state.paymentElement = null;
	    state.elements = null;

	    // Keep a clean boundary.
	    try {
	      el.innerHTML = "";
	    } catch {}
	  }

	  async function bootstrap(nextProps, token) {
	    const {
	      clientSecret,
	      returnURL,
	      publishableKey,
	      layout = "auto",
	      locale = "auto",
	      appearance,
	      business,
	      disableSubmitButton = false,
	      submitButtonText = "Pay now",
	    } = nextProps || {};

	    if (!clientSecret || !publishableKey || !returnURL) {
	      console.error("[vango-stripe] Missing clientSecret, publishableKey, or returnURL");
	      send({ event: "error", message: "Missing clientSecret, publishableKey, or returnURL" });
	      return;
	    }

	    try {
	      const stripe = await getStripe(publishableKey);
	      if (state.destroyed || token !== state.token) return;

	      const elementsOptions = { clientSecret, locale };
	      if (appearance) elementsOptions.appearance = appearance;
	      state.elements = stripe.elements(elementsOptions);

	      // Clear SSR placeholder / prior content.
	      el.innerHTML = "";

	      const mountPoint = document.createElement("div");
	      mountPoint.setAttribute("data-stripe-mount", "");
	      el.appendChild(mountPoint);

	      const paymentElementOptions = { layout };
	      if (business) paymentElementOptions.business = business;
	      state.paymentElement = state.elements.create("payment", paymentElementOptions);
	      state.paymentElement.mount(mountPoint);

	      state.paymentElement.on("ready", () => {
	        send({ event: "ready" });
	      });

	      function flushChange() {
	        state.changeTimer = null;
	        if (!state.pendingChange) return;
	        send(state.pendingChange);
	        state.pendingChange = null;
	        state.lastChangeSentAt = Date.now();
	      }

	      if (!!nextProps?.emitChangeEvents) {
	        state.paymentElement.on("change", (event) => {
	          const payload = {
	            event: "change",
	            complete: !!event?.complete,
	            empty: !!event?.empty,
	            collapsed: !!event?.collapsed,
	            value: event?.value ? { type: event.value.type } : null,
	          };

	          const now = Date.now();
	          const elapsed = now - state.lastChangeSentAt;

	          state.pendingChange = payload;

	          if (elapsed >= 250) {
	            flushChange();
	            return;
	          }
	          if (!state.changeTimer) {
	            state.changeTimer = setTimeout(flushChange, 250 - elapsed);
	          }
	        });
	      }

	      async function handleConfirm() {
	        send({ event: "confirm-started" });

	        const { error: submitError } = await state.elements.submit();
	        if (state.destroyed || token !== state.token) return;
	        if (submitError) {
	          send({
	            event: "confirm-result",
	            status: "error",
	            type: submitError.type,
	            code: submitError.code || null,
	            message: submitError.message,
	          });
	          return;
	        }

	        const { error } = await stripe.confirmPayment({
	          elements: state.elements,
	          confirmParams: { return_url: returnURL },
	          redirect: "if_required",
	        });
	        if (state.destroyed || token !== state.token) return;

	        if (error) {
	          send({
	            event: "confirm-result",
	            status: "error",
	            type: error.type,
	            code: error.code || null,
	            message: error.message,
	            declineCode: error.decline_code || null,
	          });
	        } else {
	          send({
	            event: "confirm-result",
	            status: "success",
	            message: "Payment confirmed client-side. Verify server-side.",
	          });
	        }
	      }

	      if (!disableSubmitButton) {
	        const button = document.createElement("button");
	        button.type = "button";
	        button.textContent = submitButtonText;
	        button.className = "vango-stripe-submit";
	        button.addEventListener("click", () => handleConfirm().catch((err) => {
	          console.error("[vango-stripe] confirm failed:", err);
	          send({ event: "error", message: err?.message || "confirm failed" });
	        }));
	        el.appendChild(button);
	        state.confirmButton = button;
	      }
	    } catch (err) {
	      if (state.destroyed || token !== state.token) return;
	      console.error("[vango-stripe] mount failed:", err);
	      send({ event: "error", message: err?.message || "mount failed" });
	    }
	  }

	  // First mount
	  state.key = remountKey(props);
	  bootstrap(props, state.token).catch((err) => {
	    console.error("[vango-stripe] mount failed:", err);
	    send({ event: "error", message: err?.message || "mount failed" });
	  });

	  return {
	    update(nextProps) {
	      const nextKey = remountKey(nextProps);
	      if (nextKey === state.key) return;
	      state.key = nextKey;
	      // Full teardown before remount to avoid leaks/double-mount.
	      destroy();
	      state.destroyed = false;
	      bootstrap(nextProps, state.token).catch((err) => {
	        console.error("[vango-stripe] remount failed:", err);
	        send({ event: "error", message: err?.message || "remount failed" });
	      });
	    },
	    destroy,
	  };
	}
	```

### 1.2.7 Island Client JS (`stripe-express-checkout.js`)

```javascript
// stripe-express-checkout.js
//
// Vango Island module for the Stripe Express Checkout Element.

	import { getStripe, remountKey } from "./stripe-loader.js";

	// stripe-express-checkout.js
	//
	// Vango Island module for the Stripe Express Checkout Element.

	export function mount(el, props, api) {
	  const state = {
	    token: 0,
	    key: "",
	    elements: null,
	    expressElement: null,
	    destroyed: false,
	  };

	  function send(payload) {
	    try {
	      api?.send?.(payload);
	    } catch (err) {
	      console.error("[vango-stripe] island send failed:", err);
	    }
	  }

	  function destroy() {
	    if (state.destroyed) return;
	    state.destroyed = true;
	    state.token++;
	    try {
	      state.expressElement?.unmount?.();
	    } catch {}
	    state.expressElement = null;
	    state.elements = null;
	    try {
	      el.innerHTML = "";
	    } catch {}
	  }

	  async function bootstrap(nextProps, token) {
	    const {
	      clientSecret,
	      returnURL,
	      publishableKey,
	      locale = "auto",
	      appearance,
	      buttonType = "buy",
	      buttonTheme,
	      buttonHeight = 44,
	      wallets,
	    } = nextProps || {};

	    if (!clientSecret || !publishableKey || !returnURL) {
	      console.error("[vango-stripe] Missing clientSecret, publishableKey, or returnURL");
	      send({ event: "error", message: "Missing clientSecret, publishableKey, or returnURL" });
	      return;
	    }

	    try {
	      const stripe = await getStripe(publishableKey);
	      if (state.destroyed || token !== state.token) return;

	      const elementsOptions = { clientSecret, locale };
	      if (appearance) elementsOptions.appearance = appearance;
	      state.elements = stripe.elements(elementsOptions);

	      el.innerHTML = "";
	      const mountPoint = document.createElement("div");
	      mountPoint.setAttribute("data-stripe-mount", "");
	      el.appendChild(mountPoint);

	      const expressOptions = {
	        buttonType: { googlePay: buttonType, applePay: buttonType },
	      };
	      if (buttonTheme) {
	        expressOptions.buttonTheme = {
	          googlePay: buttonTheme,
	          applePay: buttonTheme,
	        };
	      }
	      if (buttonHeight) expressOptions.buttonHeight = buttonHeight;
	      if (wallets) expressOptions.wallets = wallets;

	      state.expressElement = state.elements.create("expressCheckout", expressOptions);
	      state.expressElement.mount(mountPoint);

	      state.expressElement.on("ready", (event) => {
	        const available = event?.availablePaymentMethods || {};
	        send({ event: "ready", availablePaymentMethods: available });
	        if (Object.keys(available).length === 0) {
	          send({ event: "no-wallets" });
	        }
	      });

	      // Express Checkout confirm flow:
	      // When the user approves in the wallet sheet, the "confirm" event fires.
	      // The server MUST still verify via API/webhooks (I3).
	      //
	      // Note: `redirect: "if_required"` is a Payment Element optimization and is not used here.
	      state.expressElement.on("confirm", async (_event) => {
	        send({ event: "confirm-started" });

	        const { error: submitError } = await state.elements.submit();
	        if (state.destroyed || token !== state.token) return;
	        if (submitError) {
	          send({
	            event: "confirm-result",
	            status: "error",
	            type: submitError.type,
	            code: submitError.code || null,
	            message: submitError.message,
	          });
	          return;
	        }

	        const { error } = await stripe.confirmPayment({
	          elements: state.elements,
	          clientSecret,
	          confirmParams: { return_url: returnURL },
	        });
	        if (state.destroyed || token !== state.token) return;

	        if (error) {
	          send({
	            event: "confirm-result",
	            status: "error",
	            type: error.type,
	            code: error.code || null,
	            message: error.message,
	          });
	        } else {
	          send({
	            event: "confirm-result",
	            status: "success",
	            message: "Payment confirmed client-side. Verify server-side.",
	          });
	        }
	      });

	      state.expressElement.on("cancel", () => {
	        send({ event: "cancel" });
	      });
	    } catch (err) {
	      if (state.destroyed || token !== state.token) return;
	      console.error("[vango-stripe] express checkout mount failed:", err);
	      send({ event: "error", message: err?.message || "mount failed" });
	    }
	  }

	  state.key = remountKey(props);
	  bootstrap(props, state.token).catch((err) => {
	    console.error("[vango-stripe] express checkout mount failed:", err);
	    send({ event: "error", message: err?.message || "mount failed" });
	  });

	  return {
	    update(nextProps) {
	      const nextKey = remountKey(nextProps);
	      if (nextKey === state.key) return;
	      state.key = nextKey;
	      // Full teardown before remount to avoid leaks/double-mount.
	      destroy();
	      state.destroyed = false;
	      bootstrap(nextProps, state.token).catch((err) => {
	        console.error("[vango-stripe] express checkout remount failed:", err);
	        send({ event: "error", message: err?.message || "remount failed" });
	      });
	    },
	    destroy,
	  };
	}
	```

### 1.2.8 Island Message Protocol (Client → Server)

Vango islands send messages to the server via the island `api.send(payload)` function.
The payload becomes `msg.Raw` in the server-side handler (`OnIslandMessage` / `SetupOnIslandMessage`).

**Hard requirement (v1):** the payload MUST be a JSON object.

`vango-stripe` defines a single envelope shape with an `event` discriminator:

```json
{"event":"ready", "...":"..."}
```

This mirrors Vango's design posture elsewhere: explicit event types and explicit decoding.

**PaymentElement messages:**

| `event` | Additional fields | When |
|---|---|---|
| `ready` | — | Element mounted and interactive |
| `change` | `complete: bool`, `empty: bool`, `collapsed: bool`, `value: {type: string} \| null` | User interacts with the element (**only when** `emitChangeEvents=true`, throttled) |
| `confirm-started` | — | Submit clicked; confirmation in progress |
| `confirm-result` | `status: "success" \| "error"`, `type?: string`, `code?: string`, `message?: string`, `declineCode?: string` | `stripe.confirmPayment` resolved |
| `error` | `message: string` | Island mount/confirm failure or missing props |

**ExpressCheckoutElement messages:**

| `event` | Additional fields | When |
|---|---|---|
| `ready` | `availablePaymentMethods: {applePay?: bool, googlePay?: bool, link?: bool}` | Element mounted; reports available wallets |
| `no-wallets` | — | No wallets available in this browser |
| `confirm-started` | — | User initiated wallet payment |
| `confirm-result` | `status: "success" \| "error"`, `type?: string`, `code?: string`, `message?: string` | Confirmation resolved |
| `cancel` | — | User cancelled the wallet sheet |
| `error` | `message: string` | Island mount/confirm failure or missing props |

**Server-side handling (MUST):**

* Treat all payloads as untrusted client input (bounds + shape validation).
* `confirm-result` with `status: "success"` is a **hint** only (I3). You MUST verify via
  Stripe API calls and/or webhooks before fulfilling.
* Treat `message` fields as display-only. In production, avoid logging raw `message` strings (I5).

**Canonical Vango binding (per-instance, HID-scoped):**

Attach `OnIslandMessage` on the same element as `JSIsland(...)` so the handler is scoped
to that island instance (`msg.HID`).

```go
type StripeIslandEvent struct {
	Event  string `json:"event"`
	Status string `json:"status,omitempty"`
	Code   string `json:"code,omitempty"`
	Type   string `json:"type,omitempty"`
	// Additional fields omitted for brevity; decode explicitly per event type in real apps.
}

ui.PaymentElement(stripe.PaymentElementProps{
	ClientSecret: pi.ClientSecret,
	ReturnURL:    "https://myapp.com/checkout/complete",
},
	OnIslandMessage(func(msg vango.IslandMessage) {
		var ev StripeIslandEvent
		if err := json.Unmarshal(msg.Raw, &ev); err != nil {
			return
		}
		switch ev.Event {
		case "confirm-result":
			// ev.Status is a hint only (I3). Trigger server-side verification here if desired.
		case "error":
			// Show a UI error state; do not log secrets/PII.
		}
	}),
)
```

---

## 1.3 Webhook Handler

### 1.3.1 Core Handler

```go
package stripe

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	stripelib "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

	// NOTE: Webhook body size limits and tolerance are configured via WebhookConfig.

// EventHandler handles a verified Stripe webhook event.
//
// Handlers run in a standard HTTP context (not the Vango session loop).
// They SHOULD:
//   - Record the event for idempotency, then write to the database
//   - Enqueue long-running work rather than processing inline
//   - Be idempotent (Stripe may deliver the same event multiple times)
//
// Response code discipline:
//   - Return nil → HTTP 200. Do this for ALL accepted events, including
//     events where no matching user/order exists in your database.
//   - Return error → HTTP 500. Do this ONLY for transient infrastructure
//     failures (DB down, connection timeout) where Stripe's retry helps.
//   - Return HandlerError → custom status. Use sparingly (e.g., 503
//     during maintenance).
//   - NEVER return non-2xx for application-level "not found" — this causes
//     Stripe to retry the event for up to 3 days.
type EventHandler func(ctx *EventContext) error

// EventContext provides the verified event and request context.
type EventContext struct {
	Event     stripelib.Event
	Request   *http.Request
	RawBody   []byte
	Signature string // Stripe-Signature header value
}

// HandlerError allows handlers to control the HTTP response code.
type HandlerError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *HandlerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("stripe webhook: %s: %v", e.Message, e.Err)
	}
	return fmt.Sprintf("stripe webhook: %s", e.Message)
}

func (e *HandlerError) Unwrap() error { return e.Err }

// EventRegistration maps an event type to its handler.
type EventRegistration struct {
	EventType string
	Handler   EventHandler
}

// On creates an EventRegistration for the given Stripe event type.
func On(eventType string, handler EventHandler) EventRegistration {
	return EventRegistration{EventType: eventType, Handler: handler}
}

	// WebhookHandler returns an http.Handler that:
	//   - reads raw bytes (bounded),
	//   - verifies Stripe signatures, and
	//   - dispatches to a typed handler by event type.
	//
	// Mount as a CSRF-exempt HTTP handler on your server mux (off the Vango session loop).
	// Do not mount Stripe webhooks as `app.API(...)` routes: Vango API endpoints enforce CSRF by default.
	//
	//	mux := http.NewServeMux()
	//	mux.Handle("/webhooks/stripe", stripe.WebhookHandler(stripe.WebhookConfig{
	//	    Secret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
	//	},
	//	    stripe.On("payment_intent.succeeded", handlers.OnPaymentSucceeded),
	//	))
	//	mux.Handle("/", app)
	//	app.Server().SetHandler(mux)
	func WebhookHandler(cfg WebhookConfig, registrations ...EventRegistration) http.Handler {
		if cfg.Secret == "" {
			panic("stripe.WebhookHandler: Secret is required")
		}
		tolerance := cfg.tolerance()
		maxBody := cfg.maxBodyBytes()
		expectedLivemode := cfg.ExpectedLivemode

		handlers := make(map[string]EventHandler, len(registrations))
		for _, r := range registrations {
			if r.EventType == "" {
				panic("stripe.WebhookHandler: empty event type")
			}
			if r.Handler == nil {
				panic(fmt.Sprintf("stripe.WebhookHandler: nil handler for %q", r.EventType))
			}
			if _, exists := handlers[r.EventType]; exists {
				panic(fmt.Sprintf("stripe.WebhookHandler: duplicate handler for %q", r.EventType))
			}
			handlers[r.EventType] = r.Handler
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			defer r.Body.Close()

			body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
			if err != nil {
				http.Error(w, "Failed to read body", http.StatusBadRequest)
				return
			}
			if int64(len(body)) > maxBody {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}

		sigHeader := r.Header.Get("Stripe-Signature")
		if sigHeader == "" {
			http.Error(w, "Missing Stripe-Signature header", http.StatusBadRequest)
			return
		}

			event, err := webhook.ConstructEventWithTolerance(
				body, sigHeader, cfg.Secret, tolerance,
			)
			if err != nil {
				http.Error(w, "Invalid signature", http.StatusBadRequest)
				return
			}

			if expectedLivemode != nil && event.Livemode != *expectedLivemode {
				http.Error(w, "Invalid livemode", http.StatusBadRequest)
				return
			}

			handler, exists := handlers[string(event.Type)]
			if !exists {
				w.WriteHeader(http.StatusOK)
				return
			}

			ctx := &EventContext{
				Event:     event,
				Request:   r,
				RawBody:   body,
				Signature: sigHeader,
			}

		if err := handler(ctx); err != nil {
			var he *HandlerError
			if errors.As(err, &he) {
				http.Error(w, he.Message, he.StatusCode)
			} else {
				http.Error(w, "Internal error", http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusOK)
	})
}
```

### 1.3.2 Typed Event Unmarshaling

```go
package stripe

import (
	"encoding/json"
	"fmt"
)

// UnmarshalEventData unmarshals the event's Data.Object into the target type.
func UnmarshalEventData[T any](ctx *EventContext) (*T, error) {
	var target T
	if err := json.Unmarshal(ctx.Event.Data.Raw, &target); err != nil {
		return nil, fmt.Errorf("stripe: unmarshal %T from event %s: %w",
			target, ctx.Event.Type, err)
	}
	return &target, nil
}
```

---

## 1.4 Static Assets and UI Construction (v1)

Vango's island runtime loads ES modules from the DOM:

* Each island boundary is an element with `data-island="<id>"` (set by `JSIsland("<id>", props)`).
* By default, the thin client imports the module at:

  * `/js/islands/<id>.js`

Unless you override the module path by setting `data-module` on the same element.

**Module path override (escape hatch):** If your app fingerprints or relocates island modules,
set `data-module` on the island boundary element (same element as `JSIsland(...)`) to the correct
same-origin module URL. Example:

```go
Div(
	JSIsland("stripe-payment-element", p),
	Attr("data-module", "/assets/js/islands/stripe-payment-element.abc123.js"),
)
```

**Note:** because `stripe-payment-element.js` imports `./stripe-loader.js`, keep the loader colocated
(or adjust the import path accordingly).

### 1.4.1 Static asset contract (REQUIRED)

To use `vango-stripe` islands, your app MUST serve these files from your static directory:

* `/js/islands/stripe-payment-element.js`
* `/js/islands/stripe-express-checkout.js`
* `/js/islands/stripe-loader.js` (imported by both modules)

Because the island modules import `./stripe-loader.js`, they MUST be colocated in the
same directory.

**Recommended workflow (v1):**

* `vango create --with stripe` scaffolds these files into your app's `public/js/islands/`.
* If you are integrating manually, copy the files from the `vango-stripe` package into
  your app's static directory at the paths above.

### 1.4.2 Constructing a UI instance (RECOMMENDED)

Construct `*stripe.UI` once at app initialization and pass it via your dependency
injection wiring:

```go
ui := stripe.MustNewUI(stripe.UIConfig{
	PublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
	Locale:         "auto",
})
```

This object:

* holds client-safe defaults (publishable key, locale, appearance),
* provides render-pure helper methods (`ui.PaymentElement`, `ui.ExpressCheckoutElement`),
* does not contain secrets.

Webhook secrets are configured separately via `stripe.WebhookConfig` when constructing
the webhook handler.

---

# Part 2: Developer Guide Sections

---

## §37.9 Payments (Stripe)

### 37.9.1 Two Integration Models

**Elements (PaymentIntent + Payment Element)** — canonical path for `vango-stripe`:

- Server creates a PaymentIntent via `stripe-go` in an Action
- Render reads the Action result and passes the `ClientSecret` to the island
- User completes payment in the embedded Element
- Server verifies via API or webhook

**Checkout Sessions (redirect)** — simpler alternative, no islands needed:

- Server creates a Checkout Session in an Action
- Post-success Effect navigates to `session.URL`
- User completes payment on Stripe's hosted page
- Server verifies via webhook

See Appendix H.5 for the Checkout Sessions recipe. **Do NOT mix these models.**

### 37.9.2 The Access Rule (MUST)

> **All Stripe API calls that are part of a Vango session-driven UI MUST occur inside Resource loaders or Action work functions.**
>
> Stripe API calls from standalone HTTP handlers (for example: Stripe webhooks) are allowed because they are off the session loop.

### 37.9.3 Environment Configuration

```
STRIPE_SECRET_KEY="sk_test_..."
STRIPE_PUBLISHABLE_KEY="pk_test_..."
STRIPE_WEBHOOK_SECRET="whsec_..."
```

`STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET` are secret. Never commit or log.
`STRIPE_PUBLISHABLE_KEY` is safe for client exposure.

### 37.9.4 Dependency Injection Pattern (Canonical)

**`internal/payments/payments.go`**:

```go
package payments

import (
	"context"

	stripelib "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/client"
)

// Service defines the payment operations your application needs.
type Service interface {
	CreatePaymentIntent(ctx context.Context, params PaymentIntentParams) (*stripelib.PaymentIntent, error)
	GetPaymentIntent(ctx context.Context, id string) (*stripelib.PaymentIntent, error)
	CreateSubscription(ctx context.Context, params SubscriptionParams) (*stripelib.Subscription, error)
	GetSubscription(ctx context.Context, id string) (*stripelib.Subscription, error)
	CancelSubscription(ctx context.Context, id string) (*stripelib.Subscription, error)
	CreateBillingPortalSession(ctx context.Context, customerID, returnURL string) (*stripelib.BillingPortalSession, error)
}

type PaymentIntentParams struct {
	Amount      int64
	Currency    string
	CustomerID  string
	Description string
	Metadata    map[string]string
}

type SubscriptionParams struct {
	CustomerID string
	PriceID    string
}

type StripeService struct {
	client *client.API
}

func New(secretKey string) *StripeService {
	c := &client.API{}
	c.Init(secretKey, nil)
	return &StripeService{client: c}
}

func (s *StripeService) CreatePaymentIntent(
	ctx context.Context, p PaymentIntentParams,
) (*stripelib.PaymentIntent, error) {
	params := &stripelib.PaymentIntentParams{
		Amount:   stripelib.Int64(p.Amount),
		Currency: stripelib.String(p.Currency),
		AutomaticPaymentMethods: &stripelib.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripelib.Bool(true),
		},
	}
	if p.CustomerID != "" {
		params.Customer = stripelib.String(p.CustomerID)
	}
	if p.Description != "" {
		params.Description = stripelib.String(p.Description)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	params.Context = ctx
	return s.client.PaymentIntents.New(params)
}

func (s *StripeService) GetPaymentIntent(
	ctx context.Context, id string,
) (*stripelib.PaymentIntent, error) {
	params := &stripelib.PaymentIntentParams{}
	params.Context = ctx
	return s.client.PaymentIntents.Get(id, params)
}

func (s *StripeService) CreateSubscription(
	ctx context.Context, p SubscriptionParams,
) (*stripelib.Subscription, error) {
	params := &stripelib.SubscriptionParams{
		Customer: stripelib.String(p.CustomerID),
		Items: []*stripelib.SubscriptionItemsParams{
			{Price: stripelib.String(p.PriceID)},
		},
		PaymentBehavior: stripelib.String("default_incomplete"),
		PaymentSettings: &stripelib.SubscriptionPaymentSettingsParams{
			SaveDefaultPaymentMethod: stripelib.String("on_subscription"),
		},
	}
	params.AddExpand("latest_invoice.payment_intent")
	params.Context = ctx
	return s.client.Subscriptions.New(params)
}

func (s *StripeService) GetSubscription(
	ctx context.Context, id string,
) (*stripelib.Subscription, error) {
	params := &stripelib.SubscriptionParams{}
	params.Context = ctx
	return s.client.Subscriptions.Get(id, params)
}

func (s *StripeService) CancelSubscription(
	ctx context.Context, id string,
) (*stripelib.Subscription, error) {
	params := &stripelib.SubscriptionCancelParams{}
	params.Context = ctx
	return s.client.Subscriptions.Cancel(id, params)
}

func (s *StripeService) CreateBillingPortalSession(
	ctx context.Context, customerID, returnURL string,
) (*stripelib.BillingPortalSession, error) {
	params := &stripelib.BillingPortalSessionParams{
		Customer:  stripelib.String(customerID),
		ReturnURL: stripelib.String(returnURL),
	}
	params.Context = ctx
	return s.client.BillingPortalSessions.New(params)
}
```

**`cmd/server/main.go`** — Wiring:

```go
package main

import (
	"net/http"
	"os"

	"github.com/vango-go/vango"
	stripe "github.com/vango-go/vango-stripe"
	"myapp/app/routes"
	"myapp/internal/payments"
)

	func main() {
		// ... (config, database setup) ...

		paymentsSvc := payments.New(os.Getenv("STRIPE_SECRET_KEY"))
		stripeUI := stripe.MustNewUI(stripe.UIConfig{
			PublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
			Locale:         "auto",
		})
		// IMPORTANT: serve vango-stripe island modules at:
		//   /js/islands/stripe-payment-element.js
		//   /js/islands/stripe-express-checkout.js
		//   /js/islands/stripe-loader.js
		// See §1.4 and Appendix H.

		routes.SetDeps(routes.Deps{
			DB:       pool,
			Payments: paymentsSvc,
			StripeUI: stripeUI,
		})

		app, _ := vango.New(vango.Config{ /* ... */ })

		// Stripe webhooks must be CSRF-exempt. Mount them as standard HTTP handlers
		// on your server mux (off the Vango session loop). Do not mount Stripe webhooks
		// as `app.API(...)` routes: Vango API endpoints enforce CSRF by default.
		mux := http.NewServeMux()
		mux.Handle("/webhooks/stripe", stripe.WebhookHandler(
			stripe.WebhookConfig{Secret: os.Getenv("STRIPE_WEBHOOK_SECRET")},
			stripe.On("payment_intent.succeeded", handlers.OnPaymentSucceeded),
			stripe.On("payment_intent.payment_failed", handlers.OnPaymentFailed),
			stripe.On("customer.subscription.updated", handlers.OnSubscriptionUpdated),
			stripe.On("customer.subscription.deleted", handlers.OnSubscriptionDeleted),
			stripe.On("invoice.payment_failed", handlers.OnInvoicePaymentFailed),
			stripe.On("invoice.paid", handlers.OnInvoicePaid),
		))
		mux.Handle("/", app)
		app.Server().SetHandler(mux)

		routes.Register(app)

	// ...
}
```

### 37.9.5 Using Stripe in Resources and Actions (Elements Flow)

**Creating a PaymentIntent and rendering the Payment Element:**

The Action returns a typed result. The render closure reads the Action state directly
to decide what UI to show. No intermediate signal writes; no side effects in render.

### PaymentIntent idempotency (RECOMMENDED)

Creating PaymentIntents is commonly retried (network issues, user double-clicks, action reruns).
To avoid duplicate intents and confusing user states:

* Use a stable application idempotency key (for example: `order_id`)
* Persist your `order_id → stripe_payment_intent_id` mapping server-side

Example (service layer):

```go
params := &stripelib.PaymentIntentParams{
	Amount:   stripelib.Int64(p.Amount),
	Currency: stripelib.String(p.Currency),
	AutomaticPaymentMethods: &stripelib.PaymentIntentAutomaticPaymentMethodsParams{
		Enabled: stripelib.Bool(true),
	},
}
params.SetIdempotencyKey("order:" + p.Metadata["order_id"])
params.Context = ctx
return s.client.PaymentIntents.New(params)
```

```go
type CheckoutPageProps struct{}

func CheckoutPage(p CheckoutPageProps) vango.Component {
	return vango.Setup(p, func(s vango.SetupCtx[CheckoutPageProps]) vango.RenderFn {

		createIntent := setup.Action(&s,
			func(ctx context.Context, _ struct{}) (*stripelib.PaymentIntent, error) {
				return routes.GetDeps().Payments.CreatePaymentIntent(ctx,
					payments.PaymentIntentParams{
						Amount:   2999, // $29.99
						Currency: "usd",
						Metadata: map[string]string{"order_id": "ord_123"},
					},
				)
			},
			vango.DropWhileRunning(),
		)

		return func() *vango.VNode {
			// Render directly from action state — no intermediate signals,
			// no side effects in render (I6).
			return createIntent.Match(
				vango.OnActionIdle(func() *vango.VNode {
					return Div(
						Class("max-w-md mx-auto"),
						H2(Text("Order Summary")),
						P(Text("Total: $29.99")),
						Button(
							OnClick(func() { createIntent.Run(struct{}{}) }),
							Text("Proceed to Payment"),
						),
					)
				}),
				vango.OnActionRunning(func() *vango.VNode {
					return Div(Class("max-w-md mx-auto"),
						Text("Preparing payment…"))
				}),
					vango.OnActionSuccess(func(pi *stripelib.PaymentIntent) *vango.VNode {
						// Pure render: produce VNodes from action result.
						// No Signal.Set(), no Navigate(), no side effects.
						return Div(
							Class("max-w-md mx-auto"),
							routes.GetDeps().StripeUI.PaymentElement(stripe.PaymentElementProps{
								ClientSecret: pi.ClientSecret,
								ReturnURL:    "https://myapp.com/checkout/complete",
							}),
						)
					}),
				vango.OnActionError(func(err error) *vango.VNode {
					return Div(
						Class("max-w-md mx-auto"),
						Div(Class("text-red-600"), Text("Failed to initialize payment")),
						Button(
							OnClick(func() { createIntent.Run(struct{}{}) }),
							Text("Retry"),
						),
					)
				}),
			)
		}
	})
}
```

**Loading subscription status (Resource):**

```go
type SubscriptionStatusProps struct {
	SubscriptionID string
}

func SubscriptionStatus(p SubscriptionStatusProps) vango.Component {
	return vango.Setup(p, func(s vango.SetupCtx[SubscriptionStatusProps]) vango.RenderFn {
		props := s.Props()

		sub := setup.ResourceKeyed(&s,
			func() string { return props.Get().SubscriptionID },
			func(ctx context.Context, id string) (*stripelib.Subscription, error) {
				return routes.GetDeps().Payments.GetSubscription(ctx, id)
			},
		)

		return func() *vango.VNode {
			return sub.Match(
				vango.OnLoading(func() *vango.VNode {
					return Div(Text("Loading subscription…"))
				}),
				vango.OnError(func(err error) *vango.VNode {
					return Div(Class("text-red-600"), Text("Failed to load subscription"))
				}),
				vango.OnReady(func(s *stripelib.Subscription) *vango.VNode {
					return Div(
						P(Textf("Status: %s", s.Status)),
						P(Textf("Current period ends: %s",
							time.Unix(s.CurrentPeriodEnd, 0).Format("Jan 2, 2006"))),
					)
				}),
			)
		}
	})
}
```

### 37.9.6 The Express Checkout + Payment Element Pattern

```go
type CheckoutFormProps struct {
	ClientSecret string
}

func CheckoutForm(p CheckoutFormProps) vango.Component {
	return vango.Setup(p, func(s vango.SetupCtx[CheckoutFormProps]) vango.RenderFn {
		props := s.Props()
		walletsAvailable := setup.Signal(&s, true)

	return func() *vango.VNode {
		p := props.Get()

		return Div(
			Class("max-w-md mx-auto space-y-4"),

			routes.GetDeps().StripeUI.ExpressCheckoutElement(stripe.ExpressCheckoutProps{
				ClientSecret: p.ClientSecret,
				ReturnURL:    "https://myapp.com/checkout/complete",
			},
				OnIslandMessage(func(msg vango.IslandMessage) {
					// Treat as untrusted input; decode explicitly.
					var ev struct {
						Event                   string           `json:"event"`
						AvailablePaymentMethods map[string]bool  `json:"availablePaymentMethods,omitempty"`
					}
					if err := json.Unmarshal(msg.Raw, &ev); err != nil {
						return
					}
					switch ev.Event {
					case "ready":
						any := false
						for _, ok := range ev.AvailablePaymentMethods {
							if ok {
								any = true
								break
							}
						}
						walletsAvailable.Set(any)
					case "no-wallets":
						walletsAvailable.Set(false)
					}
				}),
			),

				func() *vango.VNode {
					if !walletsAvailable.Get() {
						return nil
					}
					return Div(Class("text-center text-gray-500 text-sm"),
						Text("— or pay with card —"))
				}(),

			routes.GetDeps().StripeUI.PaymentElement(stripe.PaymentElementProps{
				ClientSecret: p.ClientSecret,
				ReturnURL:    "https://myapp.com/checkout/complete",
			}),
		)
	}
	})
}
```

### 37.9.7 Return URL Verification Pattern

When a user returns from a redirect-based payment method, verify server-side:

```go
type CheckoutCompleteProps struct {
	PaymentIntentID string // from URL query: ?payment_intent=pi_xxx
}

func CheckoutComplete(p CheckoutCompleteProps) vango.Component {
	return vango.Setup(p, func(s vango.SetupCtx[CheckoutCompleteProps]) vango.RenderFn {
		props := s.Props()

		status := setup.ResourceKeyed(&s,
			func() string { return props.Get().PaymentIntentID },
			func(ctx context.Context, id string) (*stripelib.PaymentIntent, error) {
				return routes.GetDeps().Payments.GetPaymentIntent(ctx, id)
			},
		)

		return func() *vango.VNode {
			return status.Match(
				vango.OnLoading(func() *vango.VNode {
					return Div(Text("Verifying payment…"))
				}),
				vango.OnError(func(err error) *vango.VNode {
					return Div(Class("text-red-600"),
						Text("Unable to verify payment. Please contact support."))
				}),
				vango.OnReady(func(pi *stripelib.PaymentIntent) *vango.VNode {
					switch pi.Status {
					case stripelib.PaymentIntentStatusSucceeded:
						return Div(Class("text-green-700"),
							H2(Text("Payment successful!")),
							P(Text("Your order is being processed.")))
					case stripelib.PaymentIntentStatusProcessing:
						return Div(
							H2(Text("Payment processing")),
							P(Text("We'll notify you when confirmed.")))
					default:
						return Div(Class("text-red-600"),
							H2(Text("Payment not completed")),
							P(Textf("Status: %s. Please try again.", pi.Status)))
					}
				}),
			)
		}
	})
}
```

**Dual verification:** API verification on the return URL provides immediate feedback.
Webhooks provide reliable fulfillment triggers regardless of whether the user reaches
the return URL. Both are authoritative; ensure fulfillment is idempotent.

---

## §37.10 Webhook Handling (Stripe)

### 37.10.1 Why Webhooks Are Required

Stripe uses asynchronous webhooks for payment events. Many flows don't complete
synchronously (3D Secure, bank redirects). Webhooks are the reliable signal.

**Rule:** Never fulfill based solely on client-side callbacks. Verify via API or webhook.

### 37.10.2 Webhook Handler Registration

```go
// Stripe webhooks must be CSRF-exempt. Mount them as standard HTTP handlers
// on your server mux (off the Vango session loop). Do not mount Stripe webhooks
// as `app.API(...)` routes: Vango API endpoints enforce CSRF by default.
mux := http.NewServeMux()
mux.Handle("/webhooks/stripe", stripe.WebhookHandler(
	stripe.WebhookConfig{Secret: os.Getenv("STRIPE_WEBHOOK_SECRET")},
	stripe.On("payment_intent.succeeded", handlers.OnPaymentSucceeded),
	stripe.On("payment_intent.payment_failed", handlers.OnPaymentFailed),
	stripe.On("customer.subscription.updated", handlers.OnSubscriptionUpdated),
	stripe.On("customer.subscription.deleted", handlers.OnSubscriptionDeleted),
	stripe.On("invoice.payment_failed", handlers.OnInvoicePaymentFailed),
	stripe.On("invoice.paid", handlers.OnInvoicePaid),
))
mux.Handle("/", app)
app.Server().SetHandler(mux)
```

This avoids CSRF checks on the webhook endpoint while keeping Vango’s built-in CSRF enforcement for `app.API(...)` routes.

### 37.10.3 Writing Webhook Handlers

**Response code discipline:**

| Scenario | Response | Stripe behavior |
|---|---|---|
| Event accepted and processed | 200 (`return nil`) | No retry |
| Event accepted, no matching user | **200** (`return nil`) | No retry |
| Transient DB failure | 500 (`return err`) | Retries with backoff |
| Invalid signature | 400 (automatic) | No retry |

**Never return non-2xx for "not found" — this causes 3-day retry storms.**

```go
func OnPaymentSucceeded(ctx *stripe.EventContext) error {
	pi, err := stripe.UnmarshalEventData[stripelib.PaymentIntent](ctx)
	if err != nil {
		return err
	}

	slog.Info("payment_succeeded",
		"payment_intent_id", pi.ID,
		"amount", pi.Amount,
	)

	// Idempotent upsert
	_, dbErr := routes.GetDeps().DB.Exec(ctx.Request.Context(),
		`INSERT INTO payments (stripe_pi_id, amount, currency, status, created_at)
		 VALUES ($1, $2, $3, 'succeeded', now())
		 ON CONFLICT (stripe_pi_id) DO UPDATE SET status = 'succeeded'`,
		pi.ID, pi.Amount, string(pi.Currency),
	)
	return dbErr
}

func OnSubscriptionUpdated(ctx *stripe.EventContext) error {
	sub, err := stripe.UnmarshalEventData[stripelib.Subscription](ctx)
	if err != nil {
		return err
	}

	result, err := routes.GetDeps().DB.Exec(ctx.Request.Context(),
		`UPDATE users SET subscription_status = $1 WHERE subscription_id = $2`,
		string(sub.Status), sub.ID,
	)
	if err != nil {
		return err // transient → 500 → retry
	}

	if result.RowsAffected() == 0 {
		// No matching user — log and return 200 (NOT an error)
		slog.Warn("subscription_updated_no_match", "subscription_id", sub.ID)
	}
	return nil
}
```

### 37.10.4 Idempotency

```go
func OnPaymentSucceeded(ctx *stripe.EventContext) error {
	pi, err := stripe.UnmarshalEventData[stripelib.PaymentIntent](ctx)
	if err != nil {
		return err
	}

	// Deduplicate by event ID
	result, err := routes.GetDeps().DB.Exec(ctx.Request.Context(),
		`INSERT INTO processed_events (event_id, event_type, processed_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (event_id) DO NOTHING`,
		ctx.Event.ID, string(ctx.Event.Type),
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil // Already processed
	}

	// ... fulfill order (runs exactly once per event) ...
	return nil
}
```

---

## §37.11 Testing Payments

### 37.11.1 Service Layer Unit Testing

Mock the `payments.Service` interface:

```go
type mockPayments struct {
	createPaymentIntentFn func(ctx context.Context, p payments.PaymentIntentParams) (*stripelib.PaymentIntent, error)
	getPaymentIntentFn    func(ctx context.Context, id string) (*stripelib.PaymentIntent, error)
	// ...
}

func (m *mockPayments) CreatePaymentIntent(ctx context.Context, p payments.PaymentIntentParams) (*stripelib.PaymentIntent, error) {
	return m.createPaymentIntentFn(ctx, p)
}

// ...

func TestCheckoutPage_CreatesPaymentIntent(t *testing.T) {
	mock := &mockPayments{
		createPaymentIntentFn: func(ctx context.Context, p payments.PaymentIntentParams) (*stripelib.PaymentIntent, error) {
			if p.Amount != 2999 {
				t.Errorf("expected 2999, got %d", p.Amount)
			}
			return &stripelib.PaymentIntent{
				ID:           "pi_test_123",
				ClientSecret: "pi_test_123_secret_abc",
			}, nil
		},
	}

	routes.SetDeps(routes.Deps{
		DB:       &neon.TestDB{},
		Payments: mock,
		StripeUI: stripe.MustNewUI(stripe.UIConfig{PublishableKey: "pk_test_dummy"}),
	})

	// ... mount, trigger, assert ...
}
```

### 37.11.2 Webhook Handler Testing

```go
func TestOnPaymentSucceeded(t *testing.T) {
	var captured string
	mock := &neon.TestDB{
		ExecFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			captured = sql
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	routes.SetDeps(routes.Deps{DB: mock})

	ctx := &stripe.EventContext{
		Event: stripelib.Event{
			ID:   "evt_test_001",
			Type: "payment_intent.succeeded",
			Data: &stripelib.EventData{
				Raw: json.RawMessage(`{"id":"pi_123","amount":2999,"currency":"usd"}`),
			},
		},
		Request: httptest.NewRequest("POST", "/webhooks/stripe", nil),
	}

	if err := handlers.OnPaymentSucceeded(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(captured, "INSERT INTO payments") {
		t.Errorf("expected payment insert, got: %s", captured)
	}
}
```

**Testing signature verification (`stripe.WebhookHandler`):**

```go
func TestWebhookHandler_VerifiesSignature(t *testing.T) {
	secret := "whsec_test_123"

	// Minimal Stripe event JSON payload.
	payload := []byte(`{
	  "id": "evt_test_001",
	  "type": "payment_intent.succeeded",
	  "data": { "object": { "id": "pi_test_123" } }
	}`)

	now := time.Now()
	sig := stripeSignatureHeader(now, secret, payload)

	called := false
	h := stripe.WebhookHandler(stripe.WebhookConfig{
		Secret:    secret,
		Tolerance: 5 * time.Minute,
	},
		stripe.On("payment_intent.succeeded", func(ctx *stripe.EventContext) error {
			called = true
			if ctx.Event.ID != "evt_test_001" {
				t.Fatalf("unexpected event id: %q", ctx.Event.ID)
			}
			return nil
		}),
	)

	req := httptest.NewRequest("POST", "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", sig)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

func stripeSignatureHeader(t time.Time, secret string, payload []byte) string {
	// Stripe signature scheme (v1):
	// signedPayload = "<timestamp>.<payload>"
	ts := strconv.FormatInt(t.Unix(), 10)
	signed := ts + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signed))
	sum := hex.EncodeToString(mac.Sum(nil))
	return "t=" + ts + ",v1=" + sum
}
```

### 37.11.3 Stripe CLI for Local Development

```bash
stripe listen --forward-to localhost:8080/webhooks/stripe
stripe trigger payment_intent.succeeded
```

### 37.11.4 Integration Testing

Gate behind `//go:build integration`. Use `sk_test_*` keys and test card numbers.

---

# Part 3: Appendix H — Stripe Integration Operations

---

## H.1 Content Security Policy (CSP) Requirements

### H.1.1 Required Directives (Base)

| Directive | Required Value | Reason |
|---|---|---|
| `script-src` | `https://js.stripe.com` | Stripe.js script loading |
| `frame-src` | `https://js.stripe.com https://hooks.stripe.com` | Payment Element iframes and 3DS authentication |
| `connect-src` | `https://api.stripe.com` (often also `https://q.stripe.com`, `https://errors.stripe.com`) | Stripe.js network calls |

**Note:** Vango's CSP helper configures `script-src` and `connect-src` directly, but `frame-src`
must be added via `CSPOptions.AdditionalDirectives` (Vango's default CSP builder does not
emit a `frame-src` directive).

### H.1.2 Configuration

```go
cfg := vango.DefaultConfig()
opts := server.DefaultCSPOptions()
opts.AdditionalScriptSrc = []string{"https://js.stripe.com"}
opts.AdditionalConnectSrc = []string{
	"https://api.stripe.com",
	// Add only if you observe CSP violations:
	// "https://q.stripe.com",
	// "https://errors.stripe.com",
}
opts.AdditionalDirectives = []string{
	"frame-src https://js.stripe.com https://hooks.stripe.com",
}
cfg.Security.SecurityHeaders.CSPOptions = &opts
```

### H.1.3 Additional Payment Methods

Some local payment methods (Klarna, Afterpay, iDEAL, etc.) require additional Stripe
subdomains. Expand **only the specific directives** where you observe CSP violations:

| Directive | Expanded Value | When |
|---|---|---|
| `connect-src` | `https://*.stripe.com` | Local methods with external API calls |
| `img-src` | `https://*.stripe.com` | Payment method logos |
| `frame-src` | `https://*.stripe.com` | Bank redirect flows |

**Never widen `script-src` to a wildcard.** `https://js.stripe.com` is sufficient.

### H.1.4 Nonce/Hash-based CSP (strict posture)

If your CSP uses nonce- or hash-based `script-src` and does not allowlist `https://js.stripe.com`,
Stripe.js loading can fail.

Supported options:

1. **Preload Stripe.js in the document `<head>`** with the correct nonce, or
2. Render `<meta name="csp-nonce" content="...">` and rely on `stripe-loader.js` applying `script.nonce`.

Do not widen `script-src` to a wildcard.

---

## H.2 Operational Guardrails

### H.2.1 Webhook Requirements

| Requirement | Detail |
|---|---|
| HTTPS | Required in production |
| Publicly accessible | Stripe must reach your endpoint |
| Responds within 30s | Longer causes retry |
| Returns 2xx on success | Non-2xx retries for up to 3 days |
| Idempotent handlers | Stripe may deliver duplicates |
| CSRF exempt | Mount on the server mux (not `app.API(...)`) |
| No ordering assumptions | Events may arrive out of order |

### H.2.2 Response Code Decision Table

| Scenario | Response | Stripe behavior |
|---|---|---|
| Event accepted and processed | 200 | No retry ✓ |
| Event accepted, no matching record | **200** | No retry ✓ |
| Transient DB failure | 500 | Retries with backoff ✓ |
| Maintenance window | 503 (via `HandlerError`) | Retries with backoff ✓ |
| Invalid signature | 400 (automatic) | No retry ✓ |

### H.2.3 API Key and Webhook Secret Rotation

**API keys:** Create new → update env → deploy → delete old.

**Webhook secrets:** Create new endpoint → update env → deploy → delete old endpoint.

### H.2.4 Test vs Live Mode

| Aspect | Test | Live |
|---|---|---|
| Keys | `sk_test_*` / `pk_test_*` | `sk_live_*` / `pk_live_*` |
| Charges | Simulated | Real |
| Cards | Test numbers only | Real cards |

---

## H.3 Troubleshooting

| Scenario | Symptom | Resolution |
|---|---|---|
| Blank payment form | Missing CSP `script-src` for `js.stripe.com` | Add CSP per §H.1 |
| 3DS modal blocked | Missing `frame-src` for `hooks.stripe.com` | Add to `frame-src` |
| 403 on webhook | Webhook mounted as `app.API(...)` route | Mount webhook on the server mux (not `app.API(...)`) |
| 400 on webhook | Wrong secret or body consumed | Verify `STRIPE_WEBHOOK_SECRET` |
| Element disappears | Mounted outside island boundary | Verify `JSIsland` usage |
| Duplicate fulfillment | Handler not idempotent | Deduplicate by event ID |
| "No such PaymentIntent" | Checkout Session secret passed to Element | Use PaymentIntent secret |
| Webhook retry storm | Non-2xx for "not found" | Return 200 for accepted events |
| Element remounts constantly | Props JSON changing every render | Ensure island props are stable (no random/time values); module `update()` skips no-op remounts when `remountKey(...)` is unchanged |

---

## H.4 Phased Rollout

| Phase | Deliverable |
|---|---|
| **1** (Now) | `vango-stripe` package + Guide §37.9–§37.11 + Appendix H |
| **2** | `vango create --with stripe` scaffold |
| **3** | Additional islands (Address Element, etc.) |

---

## H.5 Checkout Sessions Recipe (Redirect Flow)

Checkout Sessions redirect the user to Stripe's hosted page. No islands needed.

**Service layer:**

```go
func (s *StripeService) CreateCheckoutSession(
	ctx context.Context, p CheckoutSessionParams,
) (*stripelib.CheckoutSession, error) {
	params := &stripelib.CheckoutSessionParams{
		Mode: stripelib.String(string(p.Mode)),
		LineItems: []*stripelib.CheckoutSessionLineItemParams{
			{
				Price:    stripelib.String(p.PriceID),
				Quantity: stripelib.Int64(1),
			},
		},
		SuccessURL: stripelib.String(p.SuccessURL),
		CancelURL:  stripelib.String(p.CancelURL),
	}
	if p.CustomerEmail != "" {
		params.CustomerEmail = stripelib.String(p.CustomerEmail)
	}
	params.Context = ctx
	return s.client.CheckoutSessions.New(params)
}
```

**Component with render-pure navigation via Effect:**

```go
func PricingPage(p vango.NoProps) vango.Component {
	return vango.Setup(p, func(s vango.SetupCtx[vango.NoProps]) vango.RenderFn {

		checkout := setup.Action(&s,
			func(ctx context.Context, priceID string) (string, error) {
				session, err := routes.GetDeps().Payments.CreateCheckoutSession(ctx,
					payments.CheckoutSessionParams{
						Mode:       "subscription",
						PriceID:    priceID,
						SuccessURL: "https://myapp.com/billing?status=success",
						CancelURL:  "https://myapp.com/pricing",
					},
				)
				if err != nil {
					return "", err
				}
				return session.URL, nil
			},
			vango.DropWhileRunning(),
		)

		// Navigation is a side effect — it belongs in a post-commit Effect,
		// NOT in a render callback. This Effect runs on the session loop
		// after commit when the action state changes.
		s.Effect(func() vango.Cleanup {
			if checkout.State() == vango.ActionSuccess {
				url := checkout.Result()
				vango.UseCtx().Navigate(url)
			}
			return nil
		})

		return func() *vango.VNode {
			// Render is pure: produces VNodes from state, no side effects.
			return Div(
				Class("max-w-md mx-auto"),
				H2(Text("Pro Plan — $29/mo")),
				Button(
					OnClick(func() { checkout.Run("price_xxx") }),
					Disabled(checkout.State() == vango.ActionRunning),
					Text("Subscribe"),
				),
				checkout.Match(
					vango.OnActionRunning(func() *vango.VNode {
						return Div(Text("Redirecting to checkout…"))
					}),
					vango.OnActionError(func(err error) *vango.VNode {
						return Div(Class("text-red-600"),
							Text("Failed to start checkout. Please try again."))
					}),
				),
			)
		}
	})
}
```

**Key difference from Elements flow:** the Action returns `session.URL` (a redirect URL).
Navigation happens in an Effect (post-commit, on the session loop), not in render.
Fulfillment is driven by webhooks (`checkout.session.completed`).

This flow requires no islands, no Stripe.js CSP, and no client-side JavaScript integration.
