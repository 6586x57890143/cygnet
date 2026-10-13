// The one email cygnet.lol sends: what it says, and that it says it's
// automatic.
import assert from "node:assert/strict";
import { test } from "node:test";

import { automatic, proof } from "../src/mail.ts";

test("a proof email: the link, its terms, and a footer that says it's automatic", () => {
  const href = "https://cygnet.lol/v/abc_def-123";
  const m = proof("silcord", "issue", href, 30, "cygnet.lol");
  assert.equal(m.subject, "confirm your email for silcord");
  // What a person reads: the plain text, and the HTML's words without its
  // markup or styles.
  const words = m.html.replace(/<style[\s\S]*?<\/style>/g, "").replace(/ style="[^"]*"/g, "").replace(/<[^>]+>/g, " ");
  assert.doesNotMatch(m.text + words, /[A-Z]{2,}|\u2014|\u2026/, "lowercase, no em dashes or ellipses");
  for (const body of [m.text, m.html]) {
    assert.ok(body.includes(href), "the link");
    assert.match(body, /make an invite on silcord/);
    assert.match(body, /expires in 30 minutes/);
    assert.match(body, /automated message from cygnet\.lol/);
    assert.match(body, /replies to this address are not read/);
    assert.match(body, /never ask you for a password/);
    assert.match(body, /cygnet\.lol\/privacy/);
  }
  assert.match(proof("silcord", "redeem", href, 30, "cygnet.lol").text, /take an invite/);
  assert.match(proof("<x>", "issue", href, 30, "cygnet.lol").html, /&#60;x&#62;/, "escaped");
  assert.deepEqual(automatic.map((h) => h.key), ["Auto-Submitted", "X-Auto-Response-Suppress"]);
});
