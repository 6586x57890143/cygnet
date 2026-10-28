import assert from "node:assert/strict";
import { test } from "node:test";
import { formatCountdown, startCountdown } from "../public/countdown.js";

test("countdown always reads minutes and two-digit seconds, rounded up", () => {
  for (const [ms, expected] of [[3_600_000, "60:00"], [1_200_000, "20:00"], [61_001, "01:02"], [60_000, "01:00"], [59_000, "00:59"], [1, "00:01"], [0, "00:00"], [-1, "00:00"], [NaN, "00:00"]] as const) {
    assert.equal(formatCountdown(ms), expected);
  }
});

test("countdown catches up after delayed ticks and expires exactly once", () => {
  let at = 10_000, expired = 0, next: (() => void) | undefined;
  const element = { dataset: { remaining: "61001" }, textContent: "" };
  const stop = startCountdown(element, {
    now: () => at,
    schedule: (fn: () => void, delay: number) => { assert.ok(delay > 0 && delay <= 1000); next = fn; return 1; },
    cancel: () => { next = undefined; },
    onExpire: () => { expired++; },
  });
  assert.equal(element.textContent, "01:02");
  at += 1;
  next!();
  assert.equal(element.textContent, "01:01");
  at += 45_000;
  next!();
  assert.equal(element.textContent, "00:16", "a background tab must use elapsed time");
  at += 16_000;
  next!();
  assert.equal(element.textContent, "00:00");
  assert.equal(expired, 1);
  next!();
  assert.equal(expired, 1);
  stop();
});

test("cancelled and invalid countdowns schedule no further updates", () => {
  let pending: (() => void) | undefined, cancelled = 0, expired = 0;
  const element = { dataset: { remaining: "2000" }, textContent: "" };
  const stop = startCountdown(element, { now: () => 0, schedule: (fn: () => void) => { pending = fn; return 1; }, cancel: () => { cancelled++; }, onExpire: () => { expired++; } });
  stop();
  pending!();
  assert.equal(cancelled, 1);
  assert.equal(expired, 0);
  for (const remaining of ["no", "-1", "Infinity"]) {
    startCountdown({ dataset: { remaining }, textContent: "" }, { schedule: () => assert.fail("invalid timer scheduled") })();
  }
  startCountdown({ dataset: { remaining: "0" }, textContent: "" }, { onExpire: () => { expired++; } })();
  assert.equal(expired, 1);
});
