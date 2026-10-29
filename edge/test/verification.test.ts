import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { runInNewContext } from "node:vm";

const script = readFileSync(new URL("../public/support-verification.js", import.meta.url), "utf8");

test("sign-in waits for verification, handles expiry and prevents empty or duplicate submissions", () => {
  const button = { disabled: true }, status = { textContent: "checking" }, token = { value: "" };
  const form = Object.assign(new EventTarget(), { querySelector: (selector: string) => selector.includes("button") ? button : selector.includes("role") ? status : token });
  const listeners = new Map<string, (event: any) => void>();
  let timeout = () => {}, resets = 0;
  const window: any = { turnstile: { reset() { resets++; } } };
  runInNewContext(script, { window, document: { querySelector: () => form }, addEventListener: (name: string, callback: (event: any) => void) => listeners.set(name, callback), setTimeout: (callback: () => void) => { timeout = callback; } });
  const submit = () => {
    const event = new Event("submit", { cancelable: true });
    form.dispatchEvent(event);
    return !event.defaultPrevented;
  };
  assert.equal(submit(), false);
  window.supportVerified("");
  assert.equal(button.disabled, true);
  timeout();
  assert.match(status.textContent, /still verifying/);
  token.value = "valid-fixture-token";
  window.supportVerified(token.value);
  assert.equal(button.disabled, false);
  timeout();
  assert.match(status.textContent, /^verified/);
  assert.equal(submit(), true);
  assert.equal(button.disabled, true, "prevent repeated button clicks while navigating");
  assert.equal(submit(), false, "a second submit event must not reuse the token");
  window.supportVerificationExpired();
  assert.equal(submit(), false, "a previous valid token must not survive expiry");
  window.supportVerified(token.value);
  listeners.get("pageshow")!({ persisted: true });
  assert.equal(button.disabled, true);
  assert.equal(resets, 1, "back navigation needs a fresh single-use token");
  listeners.get("error")!({ target: { src: "https://challenges.cloudflare.com/turnstile/v0/api.js" } });
  assert.match(status.textContent, /couldn't load/);
  assert.equal(button.disabled, true);
  token.value = "";
  window.supportVerified("callback-token-with-missing-response-field");
  assert.equal(submit(), false);
});

test("verification script leaves non-sign-in pages alone", () => {
  runInNewContext(script, { document: { querySelector: () => null } });
});
