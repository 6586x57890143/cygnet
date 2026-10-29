// Loaded before Turnstile so even a cached API script finds its callbacks.
// Siteverify still decides on the server; this keeps incomplete forms local.
(() => {
  const form = document.querySelector('form.act[action="/in"]');
  if (!form) return;
  const button = form.querySelector('button[type="submit"]');
  const status = form.querySelector('[role="status"]');
  let verified = false;
  const waiting = (message) => {
    verified = false;
    button.disabled = true;
    status.textContent = message;
  };
  window.supportVerified = (token) => {
    if (typeof token !== "string" || token.length === 0 || token.length > 2048) return;
    verified = true;
    button.disabled = false;
    status.textContent = "verified. you can sign in.";
  };
  window.supportVerificationFailed = () => {
    waiting("verification couldn't load. check your connection or content blocker, then reload this page.");
  };
  window.supportVerificationExpired = () => {
    waiting("verification is refreshing. wait a moment before signing in.");
  };
  form.addEventListener("submit", (event) => {
    const token = form.querySelector('[name="cf-turnstile-response"]')?.value;
    if (!verified || !token || token.length > 2048) {
      event.preventDefault();
      waiting("complete the verification before signing in.");
      return;
    }
    verified = false;
    button.disabled = true;
  });
  addEventListener("error", (event) => {
    if (event.target?.src?.startsWith("https://challenges.cloudflare.com/turnstile/")) window.supportVerificationFailed();
  }, true);
  addEventListener("pageshow", (event) => {
    if (event.persisted) {
      window.supportVerificationExpired();
      window.turnstile?.reset();
    }
  });
  setTimeout(() => {
    if (!verified) status.textContent = "still verifying. if no check appears, allow challenges.cloudflare.com in your content blocker and reload.";
  }, 15000);
})();
