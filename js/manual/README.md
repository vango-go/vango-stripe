# Manual Validation Checklist (Phase 2)

Use this harness to verify island lifecycle behavior in a browser.

## Start

```bash
cd vango-stripe
python3 -m http.server 8080
```

Open `http://localhost:8080/js/manual/index.html`.

## Loader checks

1. Enter a valid `pk_test_*` key.
2. Click `Run loader check`.
3. Confirm log shows `same instance: true`.
4. Clear key and re-run; confirm clear validation error message.

## Payment Element checks

1. Provide a valid `pi_*_secret_*` and publishable key.
2. Click `Mount` and confirm `ready` event appears in the log.
3. Enable `Emit change events`; interact with the element and verify `change` messages are throttled.
4. Click submit and confirm `confirm-started` then `confirm-result`.
5. Click `Update` with unchanged props and confirm no unexpected remount behavior.
6. Change `Remount key`, click `Update`, and confirm teardown/remount occurs cleanly.
7. Click `Destroy` and confirm no additional DOM updates occur.

## Express Checkout checks

1. Provide a valid `pi_*_secret_*` and publishable key.
2. Click `Mount` and confirm `ready` with `availablePaymentMethods`.
3. If no wallets are available, confirm `no-wallets` event appears.
4. Start a wallet flow and confirm `confirm-started` then `confirm-result`.
5. Cancel the wallet sheet and confirm `cancel` event.
6. Click `Destroy` during/after mount and confirm no throw, no leaked UI.

## Cancellation safety spot-check

1. Click `Mount` then immediately `Destroy` (both islands).
2. Confirm the log does not show runtime errors about mutating detached nodes.
3. Confirm the island boundary remains clean after destroy.
