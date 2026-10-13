// The one email cygnet.lol sends: a link that proves an address is the
// person's, for making or taking an invite. It reads like the pages,
// lowercase and quiet, but as a letter: whole sentences, and a footer that
// says plainly what it is. Sent through Scaleway's Transactional Email
// (index.ts, mail), marked as automatic so no mail server answers it.

export interface Letter {
  subject: string;
  text: string;
  html: string;
}

// Purpose is what the link is for, as the person started it.
export type Purpose = "issue" | "redeem";

const esc = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

// proof is the email for a link to href, for launch, good for minutes.
export function proof(launch: string, purpose: Purpose, href: string, minutes: number, site: string): Letter {
  const what = purpose === "issue" ? "make an invite" : "take an invite";
  const lead = `someone asked to ${what} on ${launch} using this email address, and if that was you, this link confirms it`;
  const terms = `the link works once and expires in ${minutes} minutes. you can open it on any device, and the page where you started will carry on by itself`;
  const notYou = "if this wasnt you, you can ignore this email and nothing will happen";
  const footer = [
    `this is an automated message from ${site}, sent because this address was used to confirm a step on ${launch}`,
    "replies to this address are not read",
    "cygnet will never ask you for a password or a code by email",
  ];
  const text = [
    "hello",
    "",
    lead,
    "",
    href,
    "",
    terms,
    "",
    notYou,
    "",
    "cygnet",
    "",
    "",
    ...footer,
    `privacy  https://${site}/privacy`,
  ].join("\n");
  const p = "margin:0 0 16px";
  const html = `<!doctype html><html><body style="margin:0;padding:0;background:#f6f4ef">
<div style="max-width:520px;margin:0 auto;padding:32px 24px;font:15px/1.6 -apple-system,'Segoe UI',Helvetica,Arial,sans-serif;color:#1c2330">
<p style="${p}">hello</p>
<p style="${p}">${esc(lead)}</p>
<p style="margin:24px 0"><a href="${esc(href)}" style="display:inline-block;padding:10px 20px;border-radius:8px;background:#1c2330;color:#f6f4ef;text-decoration:none">${esc(what)}</a></p>
<p style="${p};font-size:13px;color:#5b6574;word-break:break-all">or open this link<br><a href="${esc(href)}" style="color:#5b6574">${esc(href)}</a></p>
<p style="${p}">${esc(terms)}</p>
<p style="${p}">${esc(notYou)}</p>
<p style="margin:0 0 32px">cygnet</p>
<hr style="border:0;border-top:1px solid #dcd8cf;margin:0 0 16px">
<p style="margin:0;font-size:12px;line-height:1.6;color:#7a8290">${footer.map(esc).join("<br>")}<br><a href="https://${esc(site)}/privacy" style="color:#7a8290">privacy</a></p>
</div></body></html>`;
  return { subject: `confirm your email for ${launch}`, text, html };
}

// automatic is the headers that mark an email as sent by a machine: mail
// servers and out of office replies leave it alone (RFC 3834), and
// Outlook's auto replies too.
export const automatic = [
  { key: "Auto-Submitted", value: "auto-generated" },
  { key: "X-Auto-Response-Suppress", value: "All" },
];
