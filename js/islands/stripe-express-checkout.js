// stripe-express-checkout.js
//
// Vango Island module for the Stripe Express Checkout Element.

import { getStripe, remountKey } from "./stripe-loader.js";

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
      }

      // Express Checkout confirm flow:
      // When the user approves in the wallet sheet, the "confirm" event fires.
      // The server MUST still verify via API/webhooks.
      state.expressElement.on("confirm", () => {
        handleConfirm().catch((err) => {
          if (state.destroyed || token !== state.token) return;
          console.error("[vango-stripe] express checkout confirm failed:", err);
          send({ event: "error", message: err?.message || "confirm failed" });
        });
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
