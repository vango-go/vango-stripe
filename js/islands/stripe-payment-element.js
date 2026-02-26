// stripe-payment-element.js
//
// Vango Island module for the Stripe Payment Element.
//
// Contract: exports mount(el, props, api) and returns { update, destroy }.

import { getStripe, remountKey } from "./stripe-loader.js";

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
        button.addEventListener("click", () =>
          handleConfirm().catch((err) => {
            console.error("[vango-stripe] confirm failed:", err);
            send({ event: "error", message: err?.message || "confirm failed" });
          }),
        );
        el.appendChild(button);
        state.confirmButton = button;
      }
    } catch (err) {
      if (state.destroyed || token !== state.token) return;
      console.error("[vango-stripe] mount failed:", err);
      send({ event: "error", message: err?.message || "mount failed" });
    }
  }

  // First mount.
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

