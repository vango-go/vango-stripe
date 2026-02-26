# vango-stripe: Phased Build Plan (Maximally Thorough)

**Package goal:** ship `github.com/vango-go/vango-stripe`, a production-grade Stripe integration for Vango that (1) provides Vango Islands for Stripe Elements (Payment Element + Express Checkout Element) without VDOM patch mismatches, (2) provides a thin, secure, Vango-friendly webhook HTTP boundary handler, and (3) provides documentation/recipes that teach developers to use `stripe-go` directly within Vango’s I/O-safe boundaries.

**Primary spec (source of truth):** `/Users/collinshill/Documents/vango/vango-stripe/STRIPE_INTEGRATION.md`.

This plan is intentionally “maximally thorough”: it includes module scaffolding, implementation sequencing, file layout, test strategy, acceptance criteria mapped to explicit invariants, example-app guidance, vango-cli scaffolding, and release readiness.

---

## 0) Scope, Non-Goals, and Constraints

### 0.1 Glossary
- **Stripe Elements**: Stripe’s embedded UI components (Payment Element, Express Checkout Element) powered by Stripe.js and iframes.
- **Island**: A Vango client-owned opaque DOM subtree; Vango renders the container but never patches inside; island JS owns the DOM within the boundary.
- **Session loop**: Vango’s single-writer event loop for long-lived sessions; must never block on external I/O.
- **I/O boundary**: Vango-approved places to call external services (Resource loaders, Action work functions, standalone HTTP handlers not running on the session loop).
- **Elements flow**: PaymentIntent + Payment Element/Express Checkout Element (canonical `vango-stripe` model).
- **Checkout Sessions flow**: Redirect to Stripe-hosted checkout; no islands; documented recipe only.

### 0.2 In scope (this repo/package)
- **Go package** `package stripe`:
  - `UIConfig`, `UI`, `NewUI`, `MustNewUI`
  - Island helpers:
    - `(*UI).PaymentElement(PaymentElementProps, ...vango.Attr) *vango.VNode`
    - `(*UI).ExpressCheckoutElement(ExpressCheckoutProps, ...vango.Attr) *vango.VNode`
  - Types used in props/config:
    - `ElementsAppearance`, `ElementsBusiness`, `ExpressCheckoutWallets`
  - Webhook support:
    - `WebhookConfig` + defaults
    - `WebhookHandler(cfg, ...registrations) http.Handler`
    - `EventContext`, `EventHandler`, `HandlerError`
    - `EventRegistration`, `On(eventType, handler)`
    - `UnmarshalEventData[T any](*EventContext) (*T, error)`
- **JS islands** (as static ES modules to be served by host apps):
  - `/js/islands/stripe-loader.js`
  - `/js/islands/stripe-payment-element.js`
  - `/js/islands/stripe-express-checkout.js`
- **Documentation**:
  - Package `README.md` (quick start, wiring, CSP, operational guardrails)
  - Go package docs (`doc.go`)
  - Keep `STRIPE_INTEGRATION.md` as the full spec; add a short “How to use” in README.
- **Tests** that validate the behavioral/security contract and can run offline.

### 0.3 Out of scope (explicitly)
- Wrapping, re-exporting, or proxying `stripe-go` APIs (apps call `stripe-go` directly).
- Providing a Stripe `Client` interface/mocking library (apps define their own service interface).
- Automatically serving static assets from Vango (apps must mount static files themselves).
- SetupIntent support (v1 does not cover; do not partially implement).
- Webhook event business logic (applications implement handlers).

### 0.4 Hard constraints / non-negotiables (must not regress)
These map directly to the spec invariants (I1–I7):
- **I1 (API call boundary):** no Stripe API calls from Vango setup callbacks, render closures, event handlers (except dispatching to actions), or lifecycle callbacks; only in Resource loaders / Action work functions / standalone HTTP handlers (e.g. webhooks).
- **I2 (DOM ownership):** all Stripe Elements must be inside Islands; Vango must never patch inside mounted Stripe DOM.
- **I3 (server-authoritative payment state):** island messages are hints only; docs and examples must enforce server verification via API/webhooks before fulfillment.
- **I4 (webhook security):** verify signatures with raw bytes; CSRF-exempt; reject invalid signatures; respond promptly.
- **I5 (secrets):** never log/format/emit secret material (`sk_*`, `whsec_*`); treat Stripe errors as potentially sensitive.
- **I6 (render purity):** `ui.PaymentElement` and `ui.ExpressCheckoutElement` are deterministic and side-effect free.
- **I7 (user-gesture confirmation):** confirmation originates from client user gestures (or wallet confirm events); no “confirm later” triggered solely by server→island messages.

---

## 1) Acceptance Criteria (Auditable Invariants → Tests)

This section defines what “done” means and how it will be proven.

### 1.1 Agent Work Packages (Recommended Breakdown)

This file is intended to be “agent-friendly”: each work package below is designed to be a small, reviewable PR-sized change with clear exit criteria. The IDs can be used to coordinate multiple coding agents without stepping on each other.

- **STRIPE-P0-01:** Create `vango-stripe` Go module scaffold (`go.mod`, `doc.go`, file layout, `README.md` skeleton).
- **STRIPE-P1-01:** Implement `UIConfig`, `UI`, `NewUI`, `MustNewUI` + config tests.
- **STRIPE-P1-02:** Implement `PaymentElementProps` + `(*UI).PaymentElement` + tests.
- **STRIPE-P1-03:** Implement `ExpressCheckoutProps` + `(*UI).ExpressCheckoutElement` + tests.
- **STRIPE-P2-01:** Add JS islands in-repo (`js/islands/*`) matching the spec contract exactly.
- **STRIPE-P2-02:** Add optional `js/manual/` smoke page (no build tooling).
- **STRIPE-P3-01:** Implement webhook handler + unmarshal helper + tests (including signature helper).
- **STRIPE-P4-01:** Finish `README.md` and any godoc examples; align with `STRIPE_INTEGRATION.md`.
- **STRIPE-P5-01:** Add a `test-app/` example route set demonstrating Elements flow + webhooks (optional but strongly recommended).
- **STRIPE-P6-01:** Implement `vango create --with stripe` scaffolding (templates + tests) in Vango CLI.

### S1: Render-pure islands (I6)
- `(*UI).PaymentElement` and `(*UI).ExpressCheckoutElement` must:
  - validate inputs deterministically (panic on obviously invalid arguments per spec),
  - apply defaults from `UIConfig` without mutation of external state,
  - produce the same VNode output for the same inputs.
- **Proof:** unit tests assert deterministic prop serialization patterns and panics on invalid keys/URLs.

### S2: DOM ownership safety (I2)
- Islands must render an opaque subtree; Stripe elements must mount within the boundary only.
- **Proof:** documentation + island implementation uses `el.innerHTML = ""` and mounts into a dedicated child; no server-generated children are relied upon beyond SSR placeholder.

### S3: Webhook verification & HTTP discipline (I4)
- Raw body bytes verification; size bounded; method is POST; missing/invalid signature is 400; unknown event type is 200; handler error mapping is correct.
- **Proof:** `webhook_test.go` covers signature verification, size limits, livemode checks, response codes.

### S4: No secret leakage (I5)
- Error strings and HTTP responses from `vango-stripe` must not contain `cfg.Secret`, `sk_*`, or `whsec_*`.
- **Proof:** tests include a denylist/heuristic secret-scan for error strings and response bodies.

### S5: User gesture posture (I7)
- Island confirmation must be invoked directly from client event handlers (click or wallet confirm).
- **Proof:** JS code review + optional integration test checklist; in-code invariants explained in README.

---

## 2) Repository & Module Setup (Phase 0)

### Deliverables
- `vango-stripe/go.mod` with module path `github.com/vango-go/vango-stripe`.
- A small, well-factored file layout (single `package stripe`).
- `README.md` and `doc.go` describing the boundaries and integration workflow.
- JS island modules stored in-repo in a way that is easy to copy into host apps.

### Tasks
1. Initialize `go.mod`:
   - `module github.com/vango-go/vango-stripe`
   - `require`:
     - `github.com/vango-go/vango` (for `JSIsland`, attrs, island placeholders, handler types)
     - `github.com/stripe/stripe-go/v84` (for webhook verification and event types)
2. Establish file layout (proposed):
   - `doc.go` (package contract: I/O boundaries, islands-only rule, secret posture)
   - `config.go` (`UIConfig`, `UI`, `WebhookConfig`, defaults)
   - `ui_payment_element.go` (props + `(*UI).PaymentElement`)
   - `ui_express_checkout.go` (props + `(*UI).ExpressCheckoutElement`)
   - `types.go` (`ElementsAppearance`, `ElementsBusiness`, `ExpressCheckoutWallets`)
   - `webhook.go` (`WebhookHandler`, `EventContext`, `HandlerError`, registry)
   - `webhook_unmarshal.go` (`UnmarshalEventData[T]`)
   - `README.md` (user-facing quickstart + CSP + webhook ops)
3. JS assets layout (proposed):
   - `vango-stripe/js/islands/stripe-loader.js`
   - `vango-stripe/js/islands/stripe-payment-element.js`
   - `vango-stripe/js/islands/stripe-express-checkout.js`
   - README describes copying these into the host app’s static dir as `/js/islands/*`.
4. Define local validation commands:
   - `go test ./...`
   - `go vet ./...`

### Concrete module/file details (what agents should implement)

#### `doc.go` (package contract)
Must explicitly state:
- **I/O boundary rule (I1)** in one paragraph (“Stripe API calls only in Resources/Actions/standalone HTTP handlers”).
- **Islands-only rule (I2)** in one paragraph (“Stripe Elements must be mounted in islands to avoid VDOM patch mismatch”).
- **Server-authoritative posture (I3)** in one paragraph (“island messages are hints; verify server-side”).
- A small wiring example for:
  - constructing `*stripe.UI`,
  - rendering `ui.PaymentElement(...)`,
  - mounting `WebhookHandler` on a server mux (CSRF-exempt).

#### `README.md` (quick start)
Must include (copy-pasteable):
- Installation steps (go module + serving static JS).
- Exact static file paths that must exist under the app’s static directory.
- CSP snippets (Appendix H).
- Webhook mount snippet (server mux; CSRF-exempt) and a note about response code discipline.

#### `js/` placement decision (important for maintainability)
Keep JS modules in-repo under `vango-stripe/js/islands/` as the “source of truth”, and treat host apps as copying them verbatim into `/js/islands/`. Avoid build tooling in v1.

### Exit criteria
- `go test ./...` passes in `vango-stripe/`.
- `go vet ./...` passes in `vango-stripe/`.
- README explains:
  - why Islands are required,
  - where Stripe API calls are allowed,
  - how to serve the JS modules,
  - webhook mounting on a server mux (CSRF-exempt),
  - CSP requirements.

---

## 3) Go: UI Config + Island Render Helpers (Phase 1)

### Deliverables
- `UIConfig` and `UI` constructors enforce “client-safe config only” (publishable key).
- Render helpers implement all prop validation and defaults described in the spec.

### Tasks
1. Implement `UIConfig` + `NewUI`:
   - Require `PublishableKey`.
   - Reject non-`pk_*` keys to prevent accidental secret exposure.
   - Default locale to `"auto"`.
   - Prefer explicit, stable error strings (helpful for developers):
     - `stripe: UIConfig.PublishableKey is required`
     - `stripe: UIConfig.PublishableKey must be a publishable key (pk_*), got %q`
2. Implement `PaymentElementProps` + `(*UI).PaymentElement`:
   - Validate:
     - `ClientSecret` present and looks like `pi_*_secret_*` (reject `cs_*`).
     - `ReturnURL` present and absolute.
     - `PublishableKey` resolves to `pk_*` (prop override or UI default).
     - `Layout` in `auto|tabs|accordion` when set.
   - Apply defaults: locale, appearance, publishable key.
   - Render:
     - outer container (optional `ID`)
     - `JSIsland("stripe-payment-element", props)`
     - `IslandPlaceholder(...)`
     - ensure no side effects or I/O.
   - Concrete render contract:
     - Always include a stable class on the boundary (`vango-stripe-payment-element`) so apps can style it.
     - Attach any user-provided attrs after the base attrs to allow overriding class/style/id if needed.
     - The island props struct must have JSON tags matching the spec exactly; `ID` must be excluded from JSON (`json:"-"` or no tag).
     - Keep the SSR placeholder minimal and semantically harmless (text only; no required IDs).
3. Implement `ExpressCheckoutProps` + `(*UI).ExpressCheckoutElement`:
   - Validate:
     - same `ClientSecret`, `ReturnURL`, `PublishableKey` rules
     - `ButtonHeight` range 40–55 (if non-zero)
     - `ButtonType` and `ButtonTheme` enums
     - `Wallets` enums (`auto|never`)
   - Apply defaults and render island + placeholder.
   - Concrete render contract:
     - Use a stable class (`vango-stripe-express-checkout`).
     - `ID` excluded from JSON props.
     - `JSIsland("stripe-express-checkout", props)` island id must match the static JS module name contract.
4. Provide typed helper types:
   - `ElementsAppearance` (JSON struct tags)
   - `ElementsBusiness`
   - `ExpressCheckoutWallets`

### Concrete validation rules (must match `STRIPE_INTEGRATION.md`)

#### Publishable key safety (I5)
- Any publishable key passed to the client must start with `pk_`.
- `NewUI` must reject non-`pk_` to prevent accidental `sk_` leak.
- The render helpers must also reject non-`pk_` if `PublishableKey` prop override is used.

#### Client secret semantics hardening (integration footgun prevention)
- Reject `cs_*` secrets in both helpers (these are Checkout Sessions secrets and cannot be used with Elements).
- Enforce PaymentIntent client secret shape: starts with `pi_` and contains `_secret_`.
- Explicitly state in panic message what went wrong and what to do instead.

#### Return URL hardening
- Must parse cleanly as an absolute URL (`url.Parse` + `IsAbs()`).
- Do not attempt to “fix up” or infer base URLs; force the caller to pass a correct absolute URL.

#### Deterministic props (avoid constant remounts)
Vango’s island manager compares the raw `data-props` string to decide whether to call `update(...)`. Applications must not pass random/time-varying data in props. `vango-stripe` should:
- keep props minimal and only include what Stripe needs,
- avoid server-side autogenerated fields inside props,
- document the `RemountKey` escape hatch for apps that want explicit remount semantics.

### Tests (must be present before leaving Phase 1)
- `config_test.go`:
  - `NewUI` rejects empty key and non-`pk_*`.
- `ui_payment_element_test.go`:
  - panics on missing/invalid client secret, checkout session secret, non-absolute return URL, non-`pk_*` key.
- `ui_express_checkout_test.go`:
  - same for Express Checkout + enum/range validation.
 - Test helper conventions:
   - include `mustPanic(t, func(){ ... }, substring)` helper so panic message stays intentional.
   - avoid brittle exact-string assertions unless the message is part of the intended developer UX.

### Exit criteria
- All validations match the spec and prevent the most dangerous integration mistakes:
  - mixing Checkout Session secrets with Elements
  - leaking `sk_*` into client props
  - non-absolute return URLs

---

## 4) JS Islands: Loader + Payment Element + Express Checkout (Phase 2)

### Deliverables
- Production-safe ES module islands that match Vango’s island runtime contract:
  - `mount(el, props, api)` returns synchronously
  - supports cancellation-safe async bootstrap
  - `destroy()` is synchronous, idempotent, and non-throwing
  - optional `update(nextProps)` with remount gating
- Shared loader ensures Stripe.js script is loaded once and Stripe instances cached per publishable key.

### Tasks
1. Implement `stripe-loader.js`:
   - Single global script load promise (`stripeJsPromise`).
   - Reuse existing Stripe.js `<script>` tag if present.
   - Apply CSP nonce if `<meta name="csp-nonce">` exists.
   - Cache `Stripe` instances per publishable key (Promise-valued cache to dedupe concurrent calls).
   - `remountKey(props)`:
     - if `props.remountKey` is a non-empty string, use it (server-defined remount semantics).
     - otherwise JSON.stringify a selected subset of props that requires remounts.
   - Concrete loader invariants:
     - `getStripe(publishableKey)` must reject empty/non-string keys with a clear error.
     - If Stripe.js fails to load, the cache entry for that publishable key must be cleared to allow retries.
     - The loader must never throw synchronously during module evaluation (only inside called functions).
     - Script injection should:
       - set `async = true`,
       - apply `nonce` when provided via `<meta name="csp-nonce" content="...">`,
       - mark successful script with `data-loaded="true"` to support “existing script tag” path.
2. Implement `stripe-payment-element.js`:
   - Mount behavior:
     - resolve Stripe instance via loader
     - create `stripe.elements({ clientSecret, locale, appearance })`
     - create + mount `elements.create("payment", { layout, business })`
     - optional built-in submit button that calls:
       - `elements.submit()` then `stripe.confirmPayment({ elements, confirmParams: { return_url }, redirect: "if_required" })`
   - Message protocol: emit JSON objects with `event` discriminator:
     - `ready`, `confirm-started`, `confirm-result`, `error`
     - optional throttled `change` events when enabled
   - Update semantics:
     - compute `remountKey(nextProps)`; if key changed, fully teardown and remount.
   - Cancellation safety:
     - token/counter invalidation prevents DOM mutation after `destroy()`.
   - Concrete DOM contract inside the island boundary:
     - Clear SSR placeholder by setting `el.innerHTML = ""` before mounting.
     - Mount Stripe Element into a dedicated child node (e.g., `<div data-stripe-mount>`).
     - Add a submit button only when `disableSubmitButton !== true`.
     - Keep all event listeners attached to nodes owned by the island so `destroy()` can remove/tear down safely.
   - Concrete `change` throttling requirements:
     - When enabled, emit at most ~4 events/sec per island instance (e.g., 250ms throttle).
     - Only include safe, small fields (do not attempt to serialize Stripe event objects).
   - Error posture:
     - Catch all async errors; emit `{event:"error", message:"..."}` for UI purposes.
     - Do not assume `api.send` exists; guard it.
3. Implement `stripe-express-checkout.js`:
   - Create `elements.create("expressCheckout", options)` and mount.
   - `ready` event emits wallet availability + `no-wallets` when none.
   - `confirm` event triggers `elements.submit()` then `stripe.confirmPayment(...)`.
   - `cancel` event emits `cancel`.
   - Concrete wallet availability logic:
     - `ready` should send the `availablePaymentMethods` object (as provided by Stripe).
     - If `availablePaymentMethods` is empty, emit `no-wallets` so the server can hide the express UI.
   - Confirm flow (I7):
     - Only confirm in response to the wallet sheet confirm event (client user gesture).
4. Add a small in-repo “manual verification page” (optional but strongly recommended):
   - A static HTML file in `vango-stripe/js/manual/` that imports the islands and simulates the island API.
   - Purpose: quick smoke test without a Vango server.

### Tests / validation
Because these modules are meant to run inside browsers and Vango’s island manager, testing options are:
- **Minimum required:** JS code review checklist + a manual test script.
- **Recommended:** add a tiny Playwright or similar browser test harness *only if the repo already uses it* (avoid introducing a heavy toolchain solely for this package).

### Exit criteria
- Islands:
  - never throw unhandled promise rejections,
  - are safe when destroyed mid-bootstrap,
  - do not continuously remount under stable props,
  - produce the expected message protocol events.

---

## 5) Go: Webhook Handler (Phase 3)

### Deliverables
- `WebhookHandler` matches spec exactly:
  - bounded body read
  - signature verification with raw bytes and tolerance
  - optional livemode enforcement
  - handler dispatch by event type
  - response code discipline to prevent retry storms
- Generic `UnmarshalEventData[T]` helper.

### Tasks
1. Implement `WebhookConfig` defaults:
   - `DefaultWebhookTolerance = 300s`
   - `DefaultWebhookMaxBodyBytes = 1MB`
2. Implement `WebhookHandler`:
   - Validate config (`Secret` required).
   - Method must be POST (405 otherwise).
   - Read raw bytes bounded by `MaxBodyBytes`.
   - Require `Stripe-Signature` header; reject missing.
   - Verify using `webhook.ConstructEventWithTolerance(body, sig, secret, tolerance)`.
   - Reject livemode mismatch when `ExpectedLivemode != nil`.
   - If event type unregistered: respond 200 immediately.
   - If handler returns:
     - `nil` → 200
     - `*HandlerError` → respond `StatusCode` with `Message`
     - any other error → 500 with generic body (no sensitive error details)
   - Concrete response body posture (I5):
     - For all 400/413/405 responses generated by `WebhookHandler`, use short generic messages that do not include the signature header, raw body, or secret.
     - For handler errors:
       - `HandlerError.Message` is treated as safe-to-return-to-Stripe (not necessarily safe-to-log).
       - Generic 500 body must not include `err.Error()`.
3. Implement `EventContext` carefully:
   - Always store `Signature` as the header string (never parse and reserialize; callers may want raw form).
   - Store `RawBody` exactly as read (before JSON parsing).
3. Implement `UnmarshalEventData[T]`:
   - unmarshal from `ctx.Event.Data.Raw`
   - error message must not include raw body (avoid logging sensitive payloads).
4. Documentation alignment:
   - README must emphasize:
     - webhooks must be CSRF-exempt (mount on server mux, not `app.API(...)`)
     - return-200-for-not-found rule to prevent Stripe retry storms
     - handler idempotency patterns (dedupe by `event.ID`)

### Tests (must be present before leaving Phase 3)
1. `webhook_test.go`:
   - Valid signature calls handler and returns 200.
   - Missing signature returns 400.
   - Invalid signature returns 400.
   - Wrong method returns 405.
   - Body > max returns 413.
   - Unknown event type returns 200 without calling anything.
   - Duplicate handler registration panics.
   - `ExpectedLivemode` mismatch returns 400.
2. `webhook_error_test.go`:
   - `HandlerError` maps status code and message.
   - non-HandlerError returns 500 with generic body.
3. `secret_leak_test.go` (heuristic):
   - error strings and response bodies do not contain:
     - the configured `whsec_*` secret
      - strings matching `sk_` / `whsec_` patterns (best-effort heuristic)

### Concrete signature test helper (required for deterministic tests)

Implement a helper in tests to compute a Stripe-style `Stripe-Signature` header for `v1` signatures:

- Inputs: timestamp (`time.Time`), webhook secret, raw payload bytes
- Compute:
  - `signedPayload = "<unix_ts>.<raw_payload>"`
  - `v1 = hex(hmac_sha256(secret, signedPayload))`
  - header format: `t=<unix_ts>,v1=<hex>`

Then in tests:
- Build a minimal JSON event payload that includes:
  - `"id"`, `"type"`, `"livemode"` (when testing `ExpectedLivemode`), and `"data": {"object": {...}}`
- Send request with `bytes.NewReader(payload)` and the computed header.

This verifies the handler’s raw-bytes signature posture (I4) and guards against regressions where someone accidentally JSON-parses and re-encodes the payload before verification.

### Exit criteria
- Full webhook handler contract proven by tests.
- Documentation clearly communicates response code discipline and idempotency.

---

## 6) Documentation & Guide Integration (Phase 4)

### Deliverables
- `vango-stripe/README.md` with:
  - install steps
  - wiring patterns (UI instance, DI pattern, where API calls go)
  - static asset copying instructions
  - webhook setup + Stripe CLI usage
  - CSP guidance
  - operational guardrails (timeouts, idempotency, logging posture)
- `doc.go` summarizing the contract and pointing to the spec.
- Optional: upstream docs integration into Vango’s main guide, if desired.

### Tasks
1. Write a concise README that references `STRIPE_INTEGRATION.md` for full details.
2. Create a vango-stripe/DEVELOPER_GUIDE.md that references the main guide for full details.
   - Avoid divergence: if content exists in two places, add an explicit “single source of truth” statement and a workflow for updating both.
3. Add a “copy assets” recipe:
   - Explicit file list and required destination paths under `/js/islands/`.
   - Note about module path override using `data-module` for fingerprinted builds.
4. Add CSP section (Appendix H alignment):
   - `script-src https://js.stripe.com`
   - `frame-src https://js.stripe.com https://hooks.stripe.com`
   - `connect-src https://api.stripe.com` (+ optional q/errors stripe domains as-needed)
   - nonce-based CSP options (`<meta name="csp-nonce">` or preloading Stripe.js).

### Concrete documentation “must-include” checklists

#### README: “Elements flow” minimal recipe
Include a minimal flow that is correct and hard to misuse:
- Action creates a PaymentIntent via `stripe-go` (or via app service interface).
- Render passes `pi.ClientSecret` + absolute `ReturnURL` into `ui.PaymentElement(...)`.
- Return URL page verifies via Stripe API call in a Resource loader.
- Webhook handler also verifies/finalizes fulfillment idempotently.

#### README: “Do not do this” footguns
Include a short list that prevents common mistakes:
- Do not call Stripe APIs in render or setup callbacks (I1).
- Do not treat island “success” as fulfillment (I3).
- Do not pass Checkout Session `cs_*` secrets into Elements islands.
- Do not forget CSP `frame-src` for `hooks.stripe.com` (3DS flows).
- Do not return non-2xx for “not found” in webhook handlers (retry storms).

### Exit criteria
- A Vango developer can follow README to:
  - create PaymentIntent in an Action,
  - render Payment Element island with client secret,
  - verify on return URL via API,
  - configure webhooks with signature verification and CSRF exemption,
  - configure CSP correctly.

---

## 7) Example App & Integration Validation (Phase 5)

This phase is about confidence, not new public API surface.

### Deliverables
- A runnable example in an existing app workspace (recommended: `test-app/`) that demonstrates:
  - Elements flow (PaymentIntent + Payment Element)
  - Express Checkout + Payment Element combined pattern
  - return-url verification resource
  - webhook endpoint handling + idempotency pattern

### Tasks
1. Add example routes/components:
   - “Proceed to payment” button runs an Action that creates a PaymentIntent.
   - Render islands using `StripeUI`.
   - Handle `OnIslandMessage` to update UI state (hints only).
   - Return URL page reads `payment_intent` query param and verifies via resource.
2. Add webhook handlers:
   - minimal `payment_intent.succeeded` + `payment_intent.payment_failed`
   - dedupe by `event.ID`
3. Add a local dev recipe using Stripe CLI:
   - `stripe listen --forward-to localhost:.../webhooks/stripe`
   - `stripe trigger payment_intent.succeeded`
4. Ensure example never logs secrets and treats Stripe errors carefully.

### Concrete example app structure (recommended)

Implement the example as a small, clearly separated slice:
- `internal/payments/` (app-level Stripe service interface and Stripe-go implementation)
- `app/routes/checkout/`:
  - `CheckoutPage` (Action to create PaymentIntent; render islands)
  - `CheckoutCompletePage` (Resource to fetch PaymentIntent status from Stripe)
- `app/routes/webhooks/stripe/`:
  - `OnPaymentIntentSucceeded`, `OnPaymentIntentFailed` handlers
  - `processed_events` table pattern (or in-memory stub if no DB in example)
  - mounted via a server `http.ServeMux` (CSRF-exempt), not as `app.API(...)`

Keep it explicitly aligned with I1/I3:
- islands only render UI; they never trigger server fulfillment directly.
- server fulfillment is driven by webhook + DB idempotency.

### Exit criteria
- Developer can complete a payment in test mode and see:
  - island success hint
  - server verification on return page
  - webhook event hitting the endpoint and updating local DB/state idempotently

---

## 8) vango-cli Scaffolding (`vango create --with stripe`) (Phase 6)

This is explicitly called out as Phase 2 in the spec’s Appendix H.4; it lives in Vango core tooling, not strictly in `vango-stripe`, but planning it here prevents integration drift.

### Deliverables
- `vango create --with stripe` copies the island JS modules into a new app:
  - `public/js/islands/stripe-loader.js`
  - `public/js/islands/stripe-payment-element.js`
  - `public/js/islands/stripe-express-checkout.js`
- Template docs or comments in the scaffold:
  - environment vars for keys
  - webhook route registration
  - CSP notes

### Tasks
1. Add a `--with stripe` option to the Vango CLI create command.
2. Add templates for the three JS modules.
3. Add a minimal example component in the scaffold (optional, but increases success rate).
4. Add CLI tests (if the CLI already has a test harness) that:
   - verify files are written to expected paths
   - do not require network or Stripe keys

### Concrete scaffolding behavior requirements
- Copy the JS modules verbatim from `vango-stripe/js/islands/` into the new app’s `public/js/islands/`.
- Do not “minify” or transform; keep source readable for app developers.
- If the CLI supports templates, include:
  - `.env.example` entries for `STRIPE_SECRET_KEY`, `STRIPE_PUBLISHABLE_KEY`, `STRIPE_WEBHOOK_SECRET`
  - a commented-out webhook mount snippet (server mux; CSRF-exempt)
  - a commented CSP snippet or link to the docs section
- Scaffold must not introduce any Stripe API calls in render paths; any example should put API calls behind Action/Resource boundaries.

### Exit criteria
- A fresh app created with `--with stripe` runs and mounts islands (with developer-supplied keys).

---

## 9) Release Readiness & Maintenance (Phase 7)

### Deliverables
- Clear versioning policy (e.g., v0.x until stable).
- Upgrade strategy for `stripe-go` and Stripe.js expectations.
- Operational guardrails documented (timeouts, retries, idempotency).

### Tasks
1. Confirm `stripe-go` major pin:
   - `v84` is currently required by the spec; define how/when upgrades happen.
2. Document compatibility notes:
   - islands require ESM module serving from same origin
   - CSP directives and nonce behavior
   - `ExpectedLivemode` recommended in production/test environments
3. Add a “Security posture” section:
   - never log secrets
   - treat Stripe error strings as potentially sensitive
   - keep webhook handlers fast; offload long work
4. Add changelog discipline:
   - when public types or prop shapes change, note it explicitly (apps depend on JSON prop shapes).

### Concrete “compatibility surface” to treat as semver-relevant
- Go public API:
  - type names, field names (including JSON tags), method signatures, panic behavior (documented invariants).
- Island identifiers and module filenames:
  - `stripe-payment-element`, `stripe-express-checkout`, `stripe-loader`
  - event envelope keys and event names
- Static asset paths:
  - `/js/islands/<id>.js` default contract
  - any changes to relative import layout inside JS modules
- Webhook handler behavior:
  - response codes, body size limits, tolerance defaults

### Concrete “security regression” tests to keep forever
- Webhook handler signature verification tests (raw bytes).
- Secret leakage heuristic tests (no `whsec_` in error strings / responses).
- UI constructors reject non-`pk_` keys.

### Exit criteria
- `go test ./...` green in all involved modules (at minimum `vango-stripe`).
- Example app recipe works end-to-end in Stripe test mode.
- Documentation is coherent and points to a single source of truth.

---

## Appendix A: Audit Checklist (Pre-Release)

Use this to do a final compliance sweep.

### A.1 Invariants
- [ ] I1: No Stripe API calls on session loop paths (docs + examples).
- [ ] I2: Stripe Elements only in islands; no server-rendered Stripe DOM.
- [ ] I3: No fulfillment based on island success; server verification required.
- [ ] I4: Webhooks verify signature with raw bytes; CSRF exempt; 200 promptly.
- [ ] I5: No secret leakage in errors/responses; sensitive logging posture documented.
- [ ] I6: Render helpers are pure and deterministic.
- [ ] I7: Confirm flows initiated by client user gesture only.

### A.2 Webhook operational safety
- [ ] Unknown event types return 200.
- [ ] “Not found” in handler returns 200 (no retry storms).
- [ ] `HandlerError` used only for intentional retry signaling (503) or explicit status needs.

### A.3 Island correctness
- [ ] `destroy()` idempotent and safe mid-bootstrap.
- [ ] `update()` remount gate is stable and prevents constant remounting.
- [ ] All async errors are caught and surfaced via `error` event; no unhandled rejections.

---

## Appendix B: Implementation Notes (Agent-Facing “Gotchas”)

### B.1 Why the plan insists on panics in render helpers
The helper methods are called inside render closures. Returning errors would force the caller to branch or log from render, which invites impurity. Panics for programmer errors are acceptable here because:
- invalid keys/URLs are integration-time bugs,
- they fail fast during development,
- they prevent subtle security footguns (e.g., accidentally shipping `sk_*` to the client).

### B.2 Props that must never be included
Never include in island props:
- `sk_*` keys
- `whsec_*` secrets
- any server-only IDs that could enable privilege escalation if copied between users

### B.3 Stripe error strings are not safe logs (I5)
In `vango-stripe`, prefer:
- returning generic 500 bodies for webhook handler errors,
- documenting a structured logging posture for application code (type/code/decline_code/request_id),
- never logging raw error strings by default in library code.

---

## Appendix C: Public API Snapshot (Exact Shapes Agents Should Implement)

This appendix is intentionally redundant with `STRIPE_INTEGRATION.md`. It exists so agents can implement without hunting through the larger spec.

### C.1 Go package name and import aliases
- Package name: `package stripe`
- When importing `stripe-go`, **always alias** to avoid collisions:
  - `stripelib "github.com/stripe/stripe-go/v84"`
  - `github.com/stripe/stripe-go/v84/webhook`

### C.2 UI types (client-safe)

```go
type UIConfig struct {
	PublishableKey string
	Locale         string
	Appearance     *ElementsAppearance
}

type UI struct{ cfg UIConfig }

func NewUI(cfg UIConfig) (*UI, error)
func MustNewUI(cfg UIConfig) *UI
```

### C.3 Island props (JSON contract)

```go
type PaymentElementProps struct {
	ClientSecret        string              `json:"clientSecret"`
	ReturnURL           string              `json:"returnURL"`
	PublishableKey      string              `json:"publishableKey,omitempty"`
	RemountKey          string              `json:"remountKey,omitempty"`
	Layout              string              `json:"layout,omitempty"`
	Locale              string              `json:"locale,omitempty"`
	Appearance          *ElementsAppearance `json:"appearance,omitempty"`
	Business            *ElementsBusiness   `json:"business,omitempty"`
	EmitChangeEvents    bool                `json:"emitChangeEvents,omitempty"`
	DisableSubmitButton bool                `json:"disableSubmitButton,omitempty"`
	SubmitButtonText    string              `json:"submitButtonText,omitempty"`
	ID                  string              `json:"-"`
}

type ElementsBusiness struct {
	Name string `json:"name,omitempty"`
}

type ExpressCheckoutProps struct {
	ClientSecret   string                 `json:"clientSecret"`
	ReturnURL      string                 `json:"returnURL"`
	PublishableKey string                 `json:"publishableKey,omitempty"`
	RemountKey     string                 `json:"remountKey,omitempty"`
	Locale         string                 `json:"locale,omitempty"`
	Appearance     *ElementsAppearance    `json:"appearance,omitempty"`
	ButtonType     string                 `json:"buttonType,omitempty"`
	ButtonTheme    string                 `json:"buttonTheme,omitempty"`
	ButtonHeight   int                    `json:"buttonHeight,omitempty"`
	Wallets        *ExpressCheckoutWallets `json:"wallets,omitempty"`
	ID             string                 `json:"-"`
}

type ExpressCheckoutWallets struct {
	ApplePay  string `json:"applePay,omitempty"`
	GooglePay string `json:"googlePay,omitempty"`
	Link      string `json:"link,omitempty"`
}

type ElementsAppearance struct {
	Theme     string                       `json:"theme,omitempty"`
	Variables map[string]string            `json:"variables,omitempty"`
	Rules     map[string]map[string]string `json:"rules,omitempty"`
}
```

### C.4 Render helpers (render-pure)

```go
func (ui *UI) PaymentElement(p PaymentElementProps, attrs ...vango.Attr) *vango.VNode
func (ui *UI) ExpressCheckoutElement(p ExpressCheckoutProps, attrs ...vango.Attr) *vango.VNode
```

### C.5 Webhook types

```go
type WebhookConfig struct {
	Secret           string
	Tolerance        time.Duration
	MaxBodyBytes     int64
	ExpectedLivemode *bool
}

const DefaultWebhookTolerance = 300 * time.Second
const DefaultWebhookMaxBodyBytes = 1 << 20

type EventContext struct {
	Event     stripelib.Event
	Request   *http.Request
	RawBody   []byte
	Signature string
}

type EventHandler func(ctx *EventContext) error

type HandlerError struct {
	StatusCode int
	Message    string
	Err        error
}

type EventRegistration struct {
	EventType string
	Handler   EventHandler
}

func On(eventType string, handler EventHandler) EventRegistration
func WebhookHandler(cfg WebhookConfig, registrations ...EventRegistration) http.Handler
func UnmarshalEventData[T any](ctx *EventContext) (*T, error)
```

---

## Appendix D: JS Contract Snapshot (Exact Events and Fields)

### D.1 Island module IDs and filenames
- Island IDs used by Go helpers (must match JS): `stripe-payment-element`, `stripe-express-checkout`
- Modules must be served by the host app at:
  - `/js/islands/stripe-loader.js`
  - `/js/islands/stripe-payment-element.js`
  - `/js/islands/stripe-express-checkout.js`

### D.2 Required exports
Each module must export `mount(el, props, api)` as a named export or default export. `mount(...)` must return synchronously an object/function per Vango’s island contract.

### D.3 Payment Element → server message shapes

- `{"event":"ready"}`
- `{"event":"change","complete":true|false,"empty":true|false,"collapsed":true|false,"value":{"type":"..." }|null}` (only when `emitChangeEvents=true`, throttled)
- `{"event":"confirm-started"}`
- `{"event":"confirm-result","status":"success","message":"..."}` (hint only; server must verify)
- `{"event":"confirm-result","status":"error","type":"...","code":"..."|null,"declineCode":"..."|null,"message":"..."}`
- `{"event":"error","message":"..."}`

### D.4 Express Checkout → server message shapes

- `{"event":"ready","availablePaymentMethods":{"applePay":true|false,"googlePay":true|false,"link":true|false}}`
- `{"event":"no-wallets"}`
- `{"event":"confirm-started"}`
- `{"event":"confirm-result","status":"success","message":"..."}` (hint only; server must verify)
- `{"event":"confirm-result","status":"error","type":"...","code":"..."|null,"message":"..."}`
- `{"event":"cancel"}`
- `{"event":"error","message":"..."}`

---

## Appendix E: Test Matrix (What Must Be Covered)

This is a suggested “minimum thorough” suite. Agents should treat it as required unless there’s a clear reason not to.

### E.1 Go unit tests
- `NewUI`:
  - rejects empty key
  - rejects non-`pk_` key
- `PaymentElement`:
  - panics on `nil UI`
  - panics on empty client secret
  - panics on `cs_*` secret
  - panics on non-`pi_*_secret_*` secret
  - panics on empty return URL
  - panics on non-absolute return URL
  - panics on non-`pk_*` publishable key (prop override)
  - panics on invalid layout enum
  - applies default locale and appearance correctly (no mutation)
- `ExpressCheckoutElement`:
  - panics on `nil UI`
  - panics on invalid button type/theme
  - panics on height out of range
  - panics on invalid wallet enum values
- `WebhookHandler`:
  - 405 on non-POST + sets `Allow: POST`
  - 413 on body too large (exact behavior: `> MaxBodyBytes`)
  - 400 on missing signature header
  - 400 on invalid signature
  - 200 for unknown event type (no handler)
  - dispatches correct handler for known event type
  - rejects livemode mismatch when `ExpectedLivemode` is set
  - maps `HandlerError` to status + message
  - maps generic handler error to 500 + generic body
- Secret leakage heuristics:
  - responses and errors do not include the configured secret string

### E.2 JS manual validation checklist (at minimum)
- Loader:
  - with CSP nonce meta tag present, injected script has `nonce` set
  - Stripe.js loaded only once even with two islands
- Payment Element:
  - emits `ready`
  - confirm emits `confirm-started` then either `confirm-result` success or error
  - `destroy()` during bootstrap doesn’t throw and doesn’t mutate DOM after
- Express Checkout:
  - emits `ready` + wallet availability
  - emits `no-wallets` when none
  - confirm path emits `confirm-started` then `confirm-result`

---

## Appendix F: Suggested vango-stripe File Tree (v1)

This is a concrete, “agents can just follow it” layout.

### F.1 Go

```
vango-stripe/
  go.mod
  go.sum
  doc.go
  README.md

  config.go
  types.go
  ui_payment_element.go
  ui_express_checkout.go
  webhook.go
  webhook_unmarshal.go

  config_test.go
  ui_payment_element_test.go
  ui_express_checkout_test.go
  webhook_test.go
  webhook_error_test.go
  secret_leak_test.go
```

**`config.go` must contain:**
- `UIConfig` + `NewUI` + `MustNewUI` + `UI` struct
- `WebhookConfig` + defaults/constants + helper methods:
  - `func (c UIConfig) locale() string`
  - `func (c WebhookConfig) tolerance() time.Duration`
  - `func (c WebhookConfig) maxBodyBytes() int64`

**`types.go` must contain:**
- `ElementsAppearance`
- `ElementsBusiness`
- `ExpressCheckoutWallets`

**`ui_payment_element.go` must contain:**
- `PaymentElementProps`
- `func (ui *UI) PaymentElement(p PaymentElementProps, attrs ...vango.Attr) *vango.VNode`

**`ui_express_checkout.go` must contain:**
- `ExpressCheckoutProps`
- `func (ui *UI) ExpressCheckoutElement(p ExpressCheckoutProps, attrs ...vango.Attr) *vango.VNode`

**`webhook.go` must contain:**
- `EventHandler`, `EventContext`, `HandlerError` (+ `Unwrap() error`)
- `EventRegistration`, `On(...)`
- `WebhookHandler(...) http.Handler`

**`webhook_unmarshal.go` must contain:**
- `UnmarshalEventData[T any](*EventContext) (*T, error)`

### F.2 JS

```
vango-stripe/
  js/
    islands/
      stripe-loader.js
      stripe-payment-element.js
      stripe-express-checkout.js
    manual/              (optional)
      index.html         (optional smoke page)
      manual-island-api.js (optional)
```

Notes for agents implementing islands:
- The Vango thin client does not `await mount()`. Treat `mount()` as “start bootstrap and return controllers synchronously”.
- Vango only calls `update(nextProps)` when the raw `data-props` string differs. If an app changes props but Vango serializes to identical JSON, `update()` will not run.
- The second-level `remountKey(nextProps)` is for *the island’s* internal decision: “do we need a full teardown/remount even though update was called?”

---

## Appendix G: Concrete Test Helpers (Agents Should Reuse)

### G.1 `mustPanic` helper (Go)
All render-helper validation tests are easier to read with a panic helper:
- Assert that a function panics.
- Optionally assert the panic message contains a substring.
- Do not require exact message match unless intentionally part of DX contract.

### G.2 `mustNotContainSecrets` helper (Go)
Implement a small helper used by multiple tests to guard against secret leakage:
- Inputs: `t`, `haystack string`, `secrets ...string`
- Assert none of the provided secret literals appear.
- Optionally include a heuristic regex denylist for `sk_` and `whsec_` tokens (best-effort).

### G.3 Webhook signature helper (Go)
Centralize the Stripe signature header generation for tests:
- `stripeSignatureHeader(now, secret, payload) string`
- Use exact raw `payload` bytes in the signed payload.
