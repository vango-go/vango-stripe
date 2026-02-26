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
    let existing = document.querySelector(
      'script[src="https://js.stripe.com/v3/"]',
    );
    if (existing && existing.dataset.failed === "true") {
      existing.remove();
      existing = null;
    }

    if (existing) {
      if (window.Stripe || existing.dataset.loaded === "true") {
        existing.dataset.loaded = "true";
        resolve();
        return;
      }
      existing.addEventListener(
        "load",
        () => {
          existing.dataset.loaded = "true";
          resolve();
        },
        { once: true },
      );
      existing.addEventListener(
        "error",
        () => {
          existing.dataset.failed = "true";
          stripeJsPromise = null;
          reject(new Error("Failed to load Stripe.js"));
        },
        { once: true },
      );
      return;
    }

    const script = document.createElement("script");
    script.src = "https://js.stripe.com/v3/";
    script.async = true;

    // CSP nonce support: allow apps to render <meta name="csp-nonce" content="...">.
    const nonceMeta = document.querySelector('meta[name="csp-nonce"]');
    if (nonceMeta && nonceMeta.getAttribute("content")) {
      script.nonce = nonceMeta.getAttribute("content");
    }

    script.onload = () => {
      script.dataset.loaded = "true";
      resolve();
    };
    script.onerror = () => {
      script.dataset.failed = "true";
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
  if (typeof publishableKey !== "string" || publishableKey.trim() === "") {
    return Promise.reject(new Error("publishableKey must be a non-empty string"));
  }

  if (stripeInstances.has(publishableKey)) {
    return stripeInstances.get(publishableKey);
  }

  const promise = loadStripeJs()
    .then(() => {
      if (!window.Stripe) {
        throw new Error("Stripe.js loaded but window.Stripe is missing");
      }
      const stripe = window.Stripe(publishableKey);
      if (!stripe) {
        throw new Error("Stripe returned an invalid instance");
      }
      return stripe;
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
  const p = props && typeof props === "object" ? props : {};

  // Advanced escape hatch: allow a server-provided key to fully control remount semantics.
  // If present, this MUST be a stable string (for example: a hash of selected fields).
  if (typeof p.remountKey === "string" && p.remountKey.length > 0) {
    return p.remountKey;
  }

  return JSON.stringify({
    clientSecret: p.clientSecret,
    publishableKey: p.publishableKey,
    locale: p.locale,
    appearance: p.appearance,
    returnURL: p.returnURL,
    // Element options that affect behavior/structure should remount.
    layout: p.layout,
    business: p.business,
    emitChangeEvents: p.emitChangeEvents,
    disableSubmitButton: p.disableSubmitButton,
    submitButtonText: p.submitButtonText,
    buttonType: p.buttonType,
    buttonTheme: p.buttonTheme,
    buttonHeight: p.buttonHeight,
    wallets: p.wallets,
  });
}
