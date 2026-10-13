// What the Worker keeps in D1: what cygnet sends for a launch's page, the
// keyed hashes that stop one person signing up twice, the emailed links
// that prove an address, the email budget, and the flock in the footer.
// A launch's line, its invites and their ledger are cygnet's; the Worker
// asks her for them (hub.ts).

import { day, fallback, launches, type Launch, type Stage } from "./signup.ts";

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

// Hashes are a person's keyed identifiers, as a sign up keeps them.
export interface Hashes {
  email: string;
  ip: string;
  net: string;
  geo: string | null;
}

export class Store {
  private db: DB;

  constructor(db: DB) {
    this.db = db;
  }

  // launch is the launch called name: the code's, with what cygnet sent
  // for it, or one cygnet registered alone.
  async launch(name: string): Promise<Launch | undefined> {
    const row = await this.db
      .prepare("SELECT guild, title, icon, admins, stage, opens FROM launch WHERE name = ?1")
      .bind(name)
      .first<{ guild: string; title: string; icon: string; admins: string; stage: Stage; opens: number | null }>();
    const known = launches[name];
    if (!row) return known;
    return { ...fallback, ...known, name, guild: row.guild, title: row.title, icon: row.icon, admins: row.admins ? row.admins.split(",") : [], stage: row.stage, opens: row.opens ?? undefined };
  }

  async launches(): Promise<Launch[]> {
    const { results } = await this.db.prepare("SELECT name FROM launch").all<{ name: string }>();
    const names = new Set([...Object.keys(launches), ...results.map((r) => r.name)]);
    return (await Promise.all([...names].map((n) => this.launch(n)))).filter((l): l is Launch => !!l);
  }

  // register keeps what cygnet sent for a launch's page. A launch the code
  // names keeps its server: another can't take its name.
  async register(name: string, guild: string, title: string, icon: string, now: number, admins: string[] = [], stage: Stage = "gathering", opens: number | null = null): Promise<boolean> {
    const known = launches[name];
    if (known?.guild && known.guild !== guild) return false;
    const taken = await this.db.prepare("SELECT guild FROM launch WHERE name = ?1").bind(name).first<{ guild: string }>();
    if (taken && taken.guild !== guild) return false;
    await this.db
      .prepare(
        `INSERT INTO launch (name, guild, title, icon, at, admins, stage, opens) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
        ON CONFLICT (name) DO UPDATE SET title = ?3, icon = ?4, at = ?5, admins = ?6, stage = ?7, opens = ?8`,
      )
      .bind(name, guild, title, icon, now, admins.join(","), stage, opens)
      .run();
    return true;
  }

  // seen is whether anyone signed up for l shares this connection, or this
  // network and area.
  async seen(l: Launch, h: Pick<Hashes, "ip" | "net" | "geo">): Promise<{ ip: boolean; area: boolean }> {
    const r = await this.db
      .prepare(
        `SELECT EXISTS (SELECT 1 FROM signup WHERE launch = ?1 AND ip = ?2) AS ip,
        EXISTS (SELECT 1 FROM signup WHERE launch = ?1 AND net = ?3 AND geo = ?4) AS area`,
      )
      .bind(l.name, h.ip, h.net, h.geo)
      .first<{ ip: number; area: number }>();
    return { ip: !!r?.ip, area: !!r?.area };
  }

  // signup is what's kept here of user's sign up for l, if they signed up.
  async signup(l: Launch, user: string) {
    return this.db
      .prepare("SELECT email, ip, net, geo FROM signup WHERE launch = ?1 AND user = ?2")
      .bind(l.name, user)
      .first<{ email: string | null; ip: string | null; net: string | null; geo: string | null }>();
  }

  async emailTaken(l: Launch, email: string): Promise<boolean> {
    return !!(await this.db.prepare("SELECT 1 FROM signup WHERE launch = ?1 AND email = ?2").bind(l.name, email).first());
  }

  // keep records user's sign up once cygnet has them on the list. Kept
  // again, it changes nothing: cygnet's answer is what counts.
  async keep(l: Launch, user: string, h: Hashes, now: number): Promise<void> {
    await this.db
      .prepare("INSERT INTO signup (launch, user, email, ip, net, geo, at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7) ON CONFLICT DO NOTHING")
      .bind(l.name, user, h.email, h.ip, h.net, h.geo, now)
      .run();
  }

  // drop forgets user's sign up for l, and any link they were sent.
  async drop(l: Launch, user: string): Promise<void> {
    await this.db.batch([
      this.db.prepare("DELETE FROM challenge WHERE launch = ?1 AND user = ?2").bind(l.name, user),
      this.db.prepare("DELETE FROM signup WHERE launch = ?1 AND user = ?2").bind(l.name, user),
    ]);
  }

  // dropEverywhere forgets user's sign up for every launch, and any link
  // they were sent.
  async dropEverywhere(user: string): Promise<void> {
    await this.db.batch([
      this.db.prepare("DELETE FROM challenge WHERE user = ?1").bind(user),
      this.db.prepare("DELETE FROM signup WHERE user = ?1").bind(user),
    ]);
  }

  // total is how many signed up for l, for its page.
  async total(l: Launch): Promise<number> {
    return (await this.db.prepare("SELECT COUNT(*) AS n FROM signup WHERE launch = ?1").bind(l.name).first<{ n: number }>())?.n ?? 0;
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

  // forget drops what was kept only to stop double sign ups once it's done
  // that job, links nobody clicked, old send counts, and a launch's sign
  // ups once keepList has passed since it opened (cygnet drops its list
  // then too).
  async forget(now: number, keepNetwork: number, launches: Launch[], keepEmail: number, keepList = Infinity): Promise<void> {
    const out = [
      this.db.prepare("UPDATE signup SET ip = NULL, net = NULL, geo = NULL WHERE at < ?1 AND (ip IS NOT NULL OR net IS NOT NULL OR geo IS NOT NULL)").bind(now - keepNetwork),
      this.db.prepare("DELETE FROM challenge WHERE expires < ?1").bind(now),
      this.db.prepare("DELETE FROM sends WHERE day < ?1").bind(Math.floor(now / 86_400_000) - 62),
    ];
    for (const l of launches) {
      if (l.opens && now > l.opens + keepEmail) out.push(this.db.prepare("UPDATE signup SET email = NULL WHERE launch = ?1 AND email IS NOT NULL").bind(l.name));
      if (l.opens && now > l.opens + keepList) out.push(this.db.prepare("DELETE FROM signup WHERE launch = ?1").bind(l.name), this.db.prepare("DELETE FROM challenge WHERE launch = ?1").bind(l.name));
    }
    await this.db.batch(out);
  }
}
