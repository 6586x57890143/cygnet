// One source for the initial server text and each browser tick. Round up
// so a quote with any time left never reads 00:00.
export function formatCountdown(ms) {
  const seconds = Math.max(0, Math.ceil(Number.isFinite(ms) ? ms / 1000 : 0));
  return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}

// Use elapsed time, never decrement a counter: delayed background timers
// catch up in one update. Wall-clock changes cannot extend the quote.
export function startCountdown(element, { now = () => performance.now(), schedule = setTimeout, cancel = clearTimeout, onExpire = () => {} } = {}) {
  const duration = Number(element.dataset.remaining);
  if (!Number.isFinite(duration) || duration < 0) return () => {};
  const started = now();
  let stopped = false, pending;
  const tick = () => {
    if (stopped) return;
    const left = Math.max(0, duration - Math.max(0, now() - started));
    element.textContent = formatCountdown(left);
    if (left === 0) {
      stopped = true;
      onExpire();
      return;
    }
    pending = schedule(tick, left % 1000 || 1000);
  };
  tick();
  return () => { stopped = true; if (pending !== undefined) cancel(pending); };
}
