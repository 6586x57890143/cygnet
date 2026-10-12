// Every read and write of a launch's list, and the ledger each write lands
// in. A write and its ledger entry go in one batch, which D1 runs as one
// transaction, and each statement after the first is guarded on the first
// having done what it should, so nothing changes without its entry.

import { day, digest, fallback, inviteCode, inviteLife, keyed, launches, type Launch, type Stage } from "./signup.ts";

// Bird is one bird of the flock in the footer: its name, and the version
// of its picture.
export interface Bird {
  name: string;
  v: string;
}

// DB is what this needs of D1, so a test can hand it SQLite.
export interface DB {
  prepare(query: string): Stmt;
  batch(statements: Stmt[]): Promise<unknown[]>;
}
export interface Stmt {
  bind(...values: unknown[]): Stmt;
  first<T = Record<string, unknown>>(): Promise<T | null>;
  all<T = Record<string, unknown>>(): Promise<{ results: T[] }>;
  run(): Promise<unknown>;
}

// Hashes are a person's keyed identifiers, as join and redeem store them.
export interface Hashes {
  email: string;
  ip: string;
  net: string;
  geo: string | null;
}

// Entry is one ledger line before it's chained.
interface Entry {
  launch: string;
  kind: string;
  actor: string;
  subject: string;
  ref: string;
  at: number;
}

// line is what an entry's mac is over: the one before it, then the entry.
const line = (prev: string, seq: number, e: Entry) => [prev, seq, e.launch, e.kind, e.actor, e.subject, e.ref, e.at].join("|");

export class Store {
  private db: DB;
  private key: string;

  constructor(db: DB, key: string) {
    this.db = db;
    this.key = key;
  }

  // launch is the launch called name: the code's, with what cygnet sent
  // for it, or one cygnet registered alone.
  async launch(name: string): Promise<Launch | undefined> {
    const row = await this.db.prepare("SELECT guild, title, icon, admins FROM launch WHERE name = ?1").bind(name).first<{ guild: string; title: string; icon: string; admins: string }>();
    const known = launches[name];
    if (!row && !known) return undefined;
    const c = await this.db.prepare("SELECT stage, opens FROM campaign WHERE name = ?1").bind(name).first<{ stage: Stage; opens: number | null }>();
    const base = row ? { ...fallback, ...known, name, guild: row.guild, title: row.title, icon: row.icon, admins: row.admins ? row.admins.split(",") : [] } : known!;
    return { ...base, stage: c?.stage ?? "gathering", opens: c?.opens ?? base.opens };
  }

  // byGuild is the launch a server has, if it has one.
  async byGuild(guild: string): Promise<Launch | undefined> {
    const row = await this.db.prepare("SELECT name FROM launch WHERE guild = ?1").bind(guild).first<{ name: string }>();
    const name = row?.name ?? Object.values(launches).find((l) => l.guild === guild)?.name;
    return name ? this.launch(name) : undefined;
  }

  // stage moves a launch's campaign on, or back. The first time it opens is
  // kept: it starts the clock on deleting the launch's data.
  async stage(name: string, stage: Stage, now: number): Promise<void> {
    await this.db
      .prepare(
        `INSERT INTO campaign (name, stage, opens, at) VALUES (?1, ?2, CASE WHEN ?2 = 'open' THEN ?3 END, ?3)
        ON CONFLICT (name) DO UPDATE SET stage = ?2, at = ?3, opens = COALESCE(opens, CASE WHEN ?2 = 'open' THEN ?3 END)`,
      )
      .bind(name, stage, now)
      .run();
  }

  // stats is a launch's campaign in numbers, for whoever runs it.
  async stats(l: Launch, now: number) {
    const m = await this.db
      .prepare(
        `SELECT COUNT(*) AS total,
          SUM(sponsor IS NULL) AS in_line,
          SUM(sponsor IS NOT NULL) AS through_invites,
          SUM(held IS NOT NULL) AS held,
          SUM(released > 0) AS released_to,
          SUM(sponsor IS NULL AND held IS NULL AND released = 0) AS waiting
        FROM member WHERE launch = ?1`,
      )
      .bind(l.name)
      .first<Record<string, number | null>>();
    const i = await this.db
      .prepare("SELECT COUNT(*) AS made, SUM(spent_by IS NOT NULL) AS used, SUM(spent_by IS NULL AND revoked IS NULL AND expires > ?2) AS open FROM invite WHERE launch = ?1")
      .bind(l.name, now)
      .first<Record<string, number | null>>();
    const n = (v: number | null | undefined) => v ?? 0;
    return {
      launch: l.name,
      stage: l.stage ?? "gathering",
      opened: l.opens ? new Date(l.opens).toISOString() : null,
      total: n(m?.total),
      in_line: n(m?.in_line),
      through_invites: n(m?.through_invites),
      held: n(m?.held),
      released_to: n(m?.released_to),
      waiting: n(m?.waiting),
      invites_made: n(i?.made),
      invites_used: n(i?.used),
      invites_open: n(i?.open),
    };
  }

  async launches(): Promise<Launch[]> {
    const { results } = await this.db.prepare("SELECT name FROM launch").all<{ name: string }>();
    const names = new Set([...Object.keys(launches), ...results.map((r) => r.name)]);
    return (await Promise.all([...names].map((n) => this.launch(n)))).filter((l): l is Launch => !!l);
  }

  // register keeps what cygnet read of a launch's server. A launch the
  // code names keeps its server: another can't take its name.
  async register(name: string, guild: string, title: string, icon: string, now: number, admins: string[] = []): Promise<boolean> {
    const known = launches[name];
    if (known?.guild && known.guild !== guild) return false;
    const taken = await this.db.prepare("SELECT guild FROM launch WHERE name = ?1").bind(name).first<{ guild: string }>();
    if (taken && taken.guild !== guild) return false;
    await this.db
      .prepare("INSERT INTO launch (name, guild, title, icon, at, admins) VALUES (?1, ?2, ?3, ?4, ?5, ?6) ON CONFLICT (name) DO UPDATE SET title = ?3, icon = ?4, at = ?5, admins = ?6")
      .bind(name, guild, title, icon, now, admins.join(","))
      .run();
    return true;
  }

  // who is a Discord ID as the ledger names it.
  who(user: string): Promise<string> {
    return keyed(this.key, "user", user);
  }

  // commit runs build's statements as one batch, built against the ledger's
  // head as it is, and builds them again if another write took the next
  // place in the ledger first.
  private async commit(launch: string, build: (append: (e: Entry, when: string, ...args: unknown[]) => Promise<Stmt>) => Promise<Stmt[]>): Promise<void> {
    for (let attempt = 0; ; attempt++) {
      // seq orders the whole ledger; the chain is the launch's own.
      const top = await this.db.prepare("SELECT MAX(seq) AS seq FROM ledger").first<{ seq: number | null }>();
      const head = await this.db.prepare("SELECT mac FROM ledger WHERE launch = ?1 ORDER BY seq DESC LIMIT 1").bind(launch).first<{ mac: string }>();
      let seq = top?.seq ?? 0;
      let prev = head?.mac ?? "";
      const append = async (e: Entry, when: string, ...args: unknown[]) => {
        seq++;
        prev = await keyed(this.key, "ledger", line(prev, seq, e));
        return this.db
          .prepare(`INSERT INTO ledger (seq, launch, kind, actor, subject, ref, at, mac) SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8 WHERE ${when}`)
          .bind(seq, e.launch, e.kind, e.actor, e.subject, e.ref, e.at, prev, ...args);
      };
      try {
        await this.db.batch(await build(append));
        return;
      } catch (err) {
        if (attempt < 5 && /ledger\.seq|UNIQUE constraint failed: ledger/.test(String(err))) continue;
        throw err;
      }
    }
  }

  // join gives user the next place in l's line, or nothing if their account
  // or email is already on the list.
  async join(l: Launch, user: string, h: Hashes, held: string | null, now: number): Promise<boolean> {
    const actor = await this.who(user);
    await this.commit(l.name, async (append) => [
      this.db
        .prepare(
          `INSERT INTO member (launch, user, place, email, ip, net, geo, held, at)
          SELECT ?1, ?2, COALESCE(MAX(place), 0) + 1, ?3, ?4, ?5, ?6, ?7, ?8 FROM member WHERE launch = ?1
          ON CONFLICT DO NOTHING`,
        )
        .bind(l.name, user, h.email, h.ip, h.net, h.geo, held, now),
      await append({ launch: l.name, kind: "join", actor, subject: actor, ref: held ? "held" : "", at: now }, "EXISTS (SELECT 1 FROM member WHERE launch = ?2 AND user = ?9 AND at = ?7)", user),
    ]);
    return !!(await this.db.prepare("SELECT 1 FROM member WHERE launch = ?1 AND user = ?2 AND at = ?3").bind(l.name, user, now).first());
  }

  // seen is whether anyone on the list shares this connection, or this
  // network and area.
  async seen(l: Launch, h: Pick<Hashes, "ip" | "net" | "geo">): Promise<{ ip: boolean; area: boolean }> {
    const r = await this.db
      .prepare(
        `SELECT EXISTS (SELECT 1 FROM member WHERE launch = ?1 AND ip = ?2) AS ip,
        EXISTS (SELECT 1 FROM member WHERE launch = ?1 AND net = ?3 AND geo = ?4) AS area`,
      )
      .bind(l.name, h.ip, h.net, h.geo)
      .first<{ ip: number; area: number }>();
    return { ip: !!r?.ip, area: !!r?.area };
  }

  async member(l: Launch, user: string) {
    return this.db
      .prepare("SELECT user, place, sponsor, email, ip, released, held FROM member WHERE launch = ?1 AND user = ?2")
      .bind(l.name, user)
      .first<{ user: string; place: number; sponsor: string | null; email: string | null; ip: string | null; released: number; held: string | null }>();
  }

  async emailTaken(l: Launch, email: string): Promise<boolean> {
    return !!(await this.db.prepare("SELECT 1 FROM member WHERE launch = ?1 AND email = ?2").bind(l.name, email).first());
  }

  // bird keeps a linked bird's picture, as cygnet sends it, and that it was
  // seen now.
  async bird(name: string, png: string, v: string, now: number): Promise<void> {
    await this.db
      .prepare("INSERT INTO bird (name, png, v, first, seen) VALUES (?1, ?2, ?3, ?4, ?4) ON CONFLICT (name) DO UPDATE SET png = excluded.png, v = excluded.v, seen = excluded.seen")
      .bind(name, png, v, now)
      .run();
  }

  // flock is the birds seen in the last day, cygnet first, then in the
  // order they first linked.
  async flock(now: number): Promise<Bird[]> {
    const rows = await this.db.prepare("SELECT name, v FROM bird WHERE seen > ?1 ORDER BY name = 'cygnet' DESC, first, name").bind(now - day).all<Bird>();
    return rows.results;
  }

  // art is a bird's picture, base64, if it has one.
  async art(name: string): Promise<string | null> {
    return (await this.db.prepare("SELECT png FROM bird WHERE name = ?1").bind(name).first<{ png: string }>())?.png ?? null;
  }

  async total(l: Launch): Promise<number> {
    return (await this.db.prepare("SELECT COUNT(*) AS n FROM member WHERE launch = ?1").bind(l.name).first<{ n: number }>())?.n ?? 0;
  }

  // view is everything a member's page shows: their place among who's in
  // line (an invitee isn't in line, they come in with their sponsor), what
  // they have left to give, and each invite they made.
  async view(l: Launch, user: string, now: number) {
    const m = await this.member(l, user);
    if (!m) return null;
    const rank = m.sponsor
      ? null
      : ((await this.db.prepare("SELECT COUNT(*) AS n FROM member WHERE launch = ?1 AND sponsor IS NULL AND place <= ?2").bind(l.name, m.place).first<{ n: number }>())?.n ?? 0);
    const { results: invites } = await this.db
      .prepare("SELECT hash, made, expires, spent_at, revoked FROM invite WHERE launch = ?1 AND issuer = ?2 ORDER BY made DESC")
      .bind(l.name, user)
      .all<{ hash: string; made: number; expires: number; spent_at: number | null; revoked: number | null }>();
    const used = invites.filter((i) => !i.revoked && (i.spent_at || i.expires > now)).length;
    return { rank, sponsored: !!m.sponsor, held: m.held, released: m.released, left: Math.max(0, m.released - used), invites };
  }

  // release gives invites to each of the first count people in line who
  // haven't had any, and aren't held: the front of the line, the ones who
  // were interested earliest. One ledger entry each.
  async release(l: Launch, count: number, invites: number, now: number): Promise<number> {
    const { results } = await this.db
      .prepare("SELECT user FROM member WHERE launch = ?1 AND sponsor IS NULL AND held IS NULL AND released = 0 ORDER BY place LIMIT ?2")
      .bind(l.name, count)
      .all<{ user: string }>();
    if (!results.length) return 0;
    const subjects = await Promise.all(results.map((r) => this.who(r.user)));
    await this.commit(l.name, async (append) => {
      const out: Stmt[] = [];
      for (const [i, r] of results.entries()) {
        out.push(this.db.prepare("UPDATE member SET released = ?3 WHERE launch = ?1 AND user = ?2 AND released = 0").bind(l.name, r.user, invites));
        out.push(await append({ launch: l.name, kind: "release", actor: "admin", subject: subjects[i], ref: String(invites), at: now }, "1"));
      }
      return out;
    });
    return results.length;
  }

  // lift lets a held member's invites be released.
  async lift(l: Launch, user: string, now: number): Promise<boolean> {
    const subject = await this.who(user);
    let lifted = false;
    await this.commit(l.name, async (append) => [
      await append({ launch: l.name, kind: "lift", actor: "admin", subject, ref: "", at: now }, "EXISTS (SELECT 1 FROM member WHERE launch = ?2 AND user = ?9 AND held IS NOT NULL)", user),
      this.db.prepare("UPDATE member SET held = NULL WHERE launch = ?1 AND user = ?2 AND held IS NOT NULL").bind(l.name, user),
    ]);
    lifted = !(await this.member(l, user))?.held;
    return lifted;
  }

  // issue makes an invite from user, if they have one left to give, and
  // returns its code: the only time it exists outside the link.
  async issue(l: Launch, user: string, now: number): Promise<string | null> {
    const code = inviteCode();
    const hash = await digest(code);
    const actor = await this.who(user);
    await this.commit(l.name, async (append) => [
      this.db
        .prepare(
          `INSERT INTO invite (hash, launch, issuer, made, expires)
          SELECT ?1, ?2, ?3, ?4, ?5 FROM member m
          WHERE m.launch = ?2 AND m.user = ?3 AND m.held IS NULL AND m.released > (
            SELECT COUNT(*) FROM invite WHERE launch = ?2 AND issuer = ?3 AND revoked IS NULL AND (spent_by IS NOT NULL OR expires > ?4))`,
        )
        .bind(hash, l.name, user, now, now + inviteLife),
      await append({ launch: l.name, kind: "issue", actor, subject: "", ref: hash, at: now }, "EXISTS (SELECT 1 FROM invite WHERE hash = ?9)", hash),
    ]);
    return (await this.db.prepare("SELECT 1 FROM invite WHERE hash = ?1").bind(hash).first()) ? code : null;
  }

  // open is the invite a code names, if it can still be spent.
  async open(l: Launch, hash: string, now: number) {
    return this.db
      .prepare("SELECT issuer, expires FROM invite WHERE hash = ?1 AND launch = ?2 AND spent_by IS NULL AND revoked IS NULL AND expires > ?3")
      .bind(hash, l.name, now)
      .first<{ issuer: string; expires: number }>();
  }

  // revoke takes back an invite of user's that nobody has used.
  async revoke(l: Launch, user: string, hash: string, now: number): Promise<boolean> {
    const actor = await this.who(user);
    await this.commit(l.name, async (append) => [
      this.db.prepare("UPDATE invite SET revoked = ?4 WHERE hash = ?1 AND launch = ?2 AND issuer = ?3 AND spent_by IS NULL AND revoked IS NULL").bind(hash, l.name, user, now),
      await append({ launch: l.name, kind: "revoke", actor, subject: "", ref: hash, at: now }, "EXISTS (SELECT 1 FROM invite WHERE hash = ?9 AND revoked = ?7)", hash),
    ]);
    return !!(await this.db.prepare("SELECT 1 FROM invite WHERE hash = ?1 AND revoked = ?2").bind(hash, now).first());
  }

  // redeem spends the invite and puts user on the list under its issuer, in
  // one transaction: the invite is spent only if they get in, and they get
  // in only if it was theirs to spend. An account or email already on the
  // list fails the whole batch, so the invite stays unspent.
  async redeem(l: Launch, hash: string, user: string, h: Hashes, held: string | null, now: number): Promise<"in" | "gone" | "taken"> {
    const actor = await this.who(user);
    const sponsor = await this.db.prepare("SELECT issuer FROM invite WHERE hash = ?1").bind(hash).first<{ issuer: string }>();
    const subject = sponsor ? await this.who(sponsor.issuer) : "";
    try {
      await this.commit(l.name, async (append) => [
        this.db
          .prepare(
            `UPDATE invite SET spent_by = ?3, spent_at = ?4
            WHERE hash = ?1 AND launch = ?2 AND spent_by IS NULL AND revoked IS NULL AND expires > ?4 AND issuer <> ?3
            AND EXISTS (SELECT 1 FROM member WHERE launch = ?2 AND user = invite.issuer)`,
          )
          .bind(hash, l.name, user, now),
        this.db
          .prepare(
            `INSERT INTO member (launch, user, place, sponsor, email, ip, net, geo, held, at)
            SELECT ?2, ?3, (SELECT COALESCE(MAX(place), 0) + 1 FROM member WHERE launch = ?2), i.issuer, ?5, ?6, ?7, ?8, ?9, ?4
            FROM invite i WHERE i.hash = ?1 AND i.spent_by = ?3 AND i.spent_at = ?4`,
          )
          .bind(hash, l.name, user, now, h.email, h.ip, h.net, h.geo, held),
        await append({ launch: l.name, kind: "redeem", actor, subject, ref: hash, at: now }, "EXISTS (SELECT 1 FROM member WHERE launch = ?2 AND user = ?9 AND at = ?7)", user),
      ]);
    } catch (err) {
      if (/UNIQUE constraint failed: member/.test(String(err))) return "taken";
      throw err;
    }
    return (await this.db.prepare("SELECT 1 FROM member WHERE launch = ?1 AND user = ?2 AND at = ?3").bind(l.name, user, now).first()) ? "in" : "gone";
  }

  // leave takes user off the list and back every invite of theirs nobody
  // has used, and leaves nothing that names them: where their Discord ID
  // stood in the record of who vouched for whom (their invites, and the
  // members they brought in) it's replaced with the keyed hash the ledger
  // knows them by. Whoever they brought in stays, under that.
  async leave(l: Launch, user: string, now: number): Promise<void> {
    const actor = await this.who(user);
    const pseudo = "h:" + actor;
    await this.commit(l.name, async (append) => [
      await append({ launch: l.name, kind: "leave", actor, subject: actor, ref: "", at: now }, "EXISTS (SELECT 1 FROM member WHERE launch = ?2 AND user = ?9)", user),
      this.db.prepare("UPDATE invite SET revoked = ?3 WHERE launch = ?1 AND issuer = ?2 AND spent_by IS NULL AND revoked IS NULL").bind(l.name, user, now),
      this.db.prepare("UPDATE invite SET issuer = ?3 WHERE launch = ?1 AND issuer = ?2").bind(l.name, user, pseudo),
      this.db.prepare("UPDATE invite SET spent_by = ?3 WHERE launch = ?1 AND spent_by = ?2").bind(l.name, user, pseudo),
      this.db.prepare("UPDATE member SET sponsor = ?3 WHERE launch = ?1 AND sponsor = ?2").bind(l.name, user, pseudo),
      this.db.prepare("DELETE FROM challenge WHERE launch = ?1 AND user = ?2").bind(l.name, user),
      this.db.prepare("DELETE FROM member WHERE launch = ?1 AND user = ?2").bind(l.name, user),
    ]);
  }

  // mine is everything kept about user for l, for them to download: what's
  // stored as is, whether each hash is, every invite they made, and every
  // ledger entry about them.
  async mine(l: Launch, user: string) {
    const m = await this.db.prepare("SELECT * FROM member WHERE launch = ?1 AND user = ?2").bind(l.name, user).first<Record<string, string | number | null>>();
    if (!m) return null;
    const { results: invites } = await this.db.prepare("SELECT made, expires, spent_at, revoked FROM invite WHERE launch = ?1 AND issuer = ?2 ORDER BY made").bind(l.name, user).all<Record<string, number | null>>();
    const h = await this.who(user);
    const { results: ledger } = await this.db
      .prepare("SELECT kind, at, actor = ?2 AS by_you FROM ledger WHERE launch = ?1 AND (actor = ?2 OR subject = ?2) ORDER BY seq")
      .bind(l.name, h)
      .all<{ kind: string; at: number; by_you: number }>();
    const when = (v: unknown) => (typeof v === "number" ? new Date(v).toISOString() : null);
    return {
      launch: l.name,
      discord_id: user,
      joined: when(m.at),
      place: m.place,
      invited_by_someone: m.sponsor !== null,
      invites_released: m.released,
      on_hold_because: m.held,
      stored_as_keyed_hashes: { email: m.email !== null, connection: m.ip !== null, network: m.net !== null, area: m.geo !== null },
      invites_made: invites.map((i) => ({ made: when(i.made), expires: when(i.expires), used: when(i.spent_at), taken_back: when(i.revoked) })),
      ledger: ledger.map((e) => ({ what: e.kind, by_you: !!e.by_you, at: when(e.at) })),
      ledger_id: h,
    };
  }

  // tree is every member with who brought them in, for an admin.
  async tree(l: Launch) {
    const { results } = await this.db
      .prepare("SELECT user, sponsor, place, released, held, at FROM member WHERE launch = ?1 ORDER BY place")
      .bind(l.name)
      .all();
    return results;
  }

  // challenge starts a proof of user's email, unless they've started too
  // many this hour. id and link are digests of the cookie's token and the
  // emailed link's.
  async challenge(c: { id: string; link: string; launch: string; user: string; purpose: "issue" | "redeem"; made: number; expires: number; email: string; invite?: string; hashes?: Omit<Hashes, "email">; held?: string | null }, perHour: number): Promise<boolean> {
    const r = await this.db
      .prepare(
        `INSERT INTO challenge (id, link, launch, user, purpose, made, expires, email, invite, ip, net, geo, held)
        SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13
        WHERE (SELECT COUNT(*) FROM challenge WHERE user = ?4 AND made > ?6 - 3600000) < ?14
        RETURNING id`,
      )
      .bind(c.id, c.link, c.launch, c.user, c.purpose, c.made, c.expires, c.email, c.invite ?? null, c.hashes?.ip ?? null, c.hashes?.net ?? null, c.hashes?.geo ?? null, c.held ?? null, perHour)
      .first();
    return !!r;
  }

  // prove marks the challenge its emailed link names as clicked.
  async prove(link: string, now: number): Promise<boolean> {
    return !!(await this.db.prepare("UPDATE challenge SET verified = ?2 WHERE link = ?1 AND verified IS NULL AND expires > ?2 RETURNING id").bind(link, now).first());
  }

  // spend takes one email from the budget, if any is left in the window:
  // one statement, so two sends at once can't both take the last one.
  async spend(now: number, emails: number, days: number): Promise<boolean> {
    const today = Math.floor(now / 86_400_000);
    return !!(await this.db
      .prepare(
        `INSERT INTO sends (day, n) SELECT ?1, 1
        WHERE (SELECT COALESCE(SUM(n), 0) FROM sends WHERE day > ?1 - ?3) < ?2
        ON CONFLICT (day) DO UPDATE SET n = n + 1
        RETURNING n`,
      )
      .bind(today, emails, days)
      .first());
  }

  // waiting is whether the challenge with id is still waiting for its
  // email, and when it stops.
  async waiting(id: string, now: number) {
    return this.db.prepare("SELECT purpose, launch, expires, verified FROM challenge WHERE id = ?1 AND expires > ?2").bind(id, now).first<{ purpose: string; launch: string; expires: number; verified: number | null }>();
  }

  // consume takes a proven challenge, once: two tabs can't both use it.
  async consume(id: string, now: number) {
    return this.db
      .prepare("DELETE FROM challenge WHERE id = ?1 AND verified IS NOT NULL AND expires > ?2 RETURNING *")
      .bind(id, now)
      .first<{ launch: string; user: string; purpose: "issue" | "redeem"; invite: string | null; email: string; ip: string; net: string; geo: string | null; held: string | null }>();
  }

  // forget drops what was kept only to stop double sign ups, links nobody
  // clicked, and a launch's whole list, invites and ledger once keepList
  // has passed since it opened.
  async forget(now: number, keepNetwork: number, launches: Launch[], keepEmail: number, keepList = Infinity): Promise<void> {
    const out = [
      this.db.prepare("UPDATE member SET ip = NULL, net = NULL, geo = NULL WHERE at < ?1 AND (ip IS NOT NULL OR net IS NOT NULL OR geo IS NOT NULL)").bind(now - keepNetwork),
      this.db.prepare("DELETE FROM challenge WHERE expires < ?1").bind(now),
      this.db.prepare("DELETE FROM sends WHERE day < ?1").bind(Math.floor(now / 86_400_000) - 62),
    ];
    for (const l of launches) {
      if (l.opens && now > l.opens + keepEmail) out.push(this.db.prepare("UPDATE member SET email = NULL WHERE launch = ?1 AND email IS NOT NULL").bind(l.name));
      if (l.opens && now > l.opens + keepList) {
        out.push(
          this.db.prepare("INSERT INTO gone (launch, at) VALUES (?1, ?2) ON CONFLICT DO NOTHING").bind(l.name, now),
          this.db.prepare("DELETE FROM member WHERE launch = ?1").bind(l.name),
          this.db.prepare("DELETE FROM invite WHERE launch = ?1").bind(l.name),
          this.db.prepare("DELETE FROM challenge WHERE launch = ?1").bind(l.name),
          this.db.prepare("DELETE FROM ledger WHERE launch = ?1").bind(l.name),
        );
      }
    }
    await this.db.batch(out);
  }

  // verify walks the ledger and checks every mac against the one before
  // it, then that each member's released invites and each issuer's
  // invites match what the ledger says happened. Anything edited, dropped
  // or added outside this code shows up as a broken seq or a mismatch.
  async verify(): Promise<{ entries: number; broken: number | null; mismatches: string[] }> {
    const { results } = await this.db.prepare("SELECT * FROM ledger ORDER BY seq").all<Entry & { seq: number; mac: string }>();
    // Each launch's chain is checked on its own: every mac over the one
    // before it in the same launch.
    const prev = new Map<string, string>();
    let broken: number | null = null;
    const released = new Map<string, number>();
    const issued = new Map<string, number>();
    for (const e of results) {
      if (e.mac !== (await keyed(this.key, "ledger", line(prev.get(e.launch) ?? "", e.seq, e)))) {
        broken = e.seq;
        break;
      }
      if (e.kind === "release") released.set(e.subject, Number(e.ref));
      if (e.kind === "issue") issued.set(e.actor, (issued.get(e.actor) ?? 0) + 1);
      prev.set(e.launch, e.mac);
    }
    const mismatches: string[] = [];
    const { results: members } = await this.db.prepare("SELECT launch, user, released FROM member").all<{ launch: string; user: string; released: number }>();
    for (const m of members) {
      const h = await this.who(m.user);
      if ((released.get(h) ?? 0) !== m.released) mismatches.push(`${m.launch}: released for ${h}`);
    }
    const { results: issuers } = await this.db.prepare("SELECT issuer, COUNT(*) AS n FROM invite GROUP BY issuer").all<{ issuer: string; n: number }>();
    const seen = new Set<string>();
    for (const i of issuers) {
      // Someone who left is already their keyed hash.
      const h = i.issuer.startsWith("h:") ? i.issuer.slice(2) : await this.who(i.issuer);
      seen.add(h);
      if ((issued.get(h) ?? 0) !== i.n) mismatches.push(`invites for ${h}`);
    }
    for (const h of issued.keys()) if (!seen.has(h)) mismatches.push(`invites for ${h}`);
    return { entries: results.length, broken, mismatches };
  }
}
