// manual-island-api.js
//
// Small helpers for manually testing vango-stripe island modules without a Vango server.

function nowIso() {
  return new Date().toISOString();
}

export function createLogger(logEl) {
  function line(message) {
    if (!logEl) return;
    logEl.textContent += `${nowIso()} ${message}\n`;
    logEl.scrollTop = logEl.scrollHeight;
  }

  function logPayload(label, payload) {
    line(`${label} ${JSON.stringify(payload)}`);
  }

  function clear() {
    if (!logEl) return;
    logEl.textContent = "";
  }

  return { line, logPayload, clear };
}

export function createMockIslandAPI(label, logger) {
  return {
    send(payload) {
      logger.logPayload(`[${label}]`, payload);
    },
  };
}

export function boolFromCheckbox(input) {
  return !!input?.checked;
}

export function intFromInput(input, fallback) {
  const raw = input?.value?.trim() ?? "";
  if (raw === "") return fallback;
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed)) return fallback;
  return parsed;
}

export function stringFromInput(input) {
  const raw = input?.value ?? "";
  const trimmed = raw.trim();
  return trimmed === "" ? "" : trimmed;
}
