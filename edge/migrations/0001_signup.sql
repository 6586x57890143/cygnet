-- A launch's list, the invites its members give, and the ledger that
-- records both. Every identifier besides a Discord ID is a keyed hash or a
-- digest, never the value.

-- member is one person on a launch's list: in line, or in through an
-- invite from sponsor, whose tree they hang from. email, ip, net and geo
-- are keyed hashes, kept only to stop one person getting in twice and
-- dropped on a timer (src/signup.ts).
CREATE TABLE member (
  launch   TEXT    NOT NULL,
  user     TEXT    NOT NULL,
  place    INTEGER NOT NULL,
  sponsor  TEXT,
  email    TEXT,
  ip       TEXT,
  net      TEXT,
  geo      TEXT,
  -- released is how many invites they've been given to hand out.
  released INTEGER NOT NULL DEFAULT 0,
  -- held is why their invites wait for an admin, or NULL.
  held     TEXT,
  at       INTEGER NOT NULL,
  PRIMARY KEY (launch, user),
  UNIQUE (launch, email),
  UNIQUE (launch, place)
);
CREATE INDEX member_ip ON member (launch, ip);
CREATE INDEX member_net ON member (launch, net, geo);
CREATE INDEX member_sponsor ON member (launch, sponsor);

-- invite is one link a member made: hash is the SHA-256 of its code, which
-- only the link holds. Spent once, by spent_by, or revoked, never both.
CREATE TABLE invite (
  hash     TEXT    PRIMARY KEY,
  launch   TEXT    NOT NULL,
  issuer   TEXT    NOT NULL,
  made     INTEGER NOT NULL,
  expires  INTEGER NOT NULL,
  spent_by TEXT,
  spent_at INTEGER,
  revoked  INTEGER,
  CHECK (spent_by IS NULL OR revoked IS NULL)
);
CREATE INDEX invite_issuer ON invite (launch, issuer);

-- challenge is a link emailed to a person, waiting to be clicked: id is
-- the SHA-256 of the token in their browser's cookie and link of the token
-- in the emailed link, so neither is any use to someone who reads this
-- table. verified is when the link was clicked; what happens then is in
-- purpose and the columns after it, and only in the browser that holds
-- the cookie.
CREATE TABLE challenge (
  id       TEXT    PRIMARY KEY,
  link     TEXT    NOT NULL UNIQUE,
  launch   TEXT    NOT NULL,
  user     TEXT    NOT NULL,
  purpose  TEXT    NOT NULL CHECK (purpose IN ('issue', 'redeem')),
  made     INTEGER NOT NULL,
  expires  INTEGER NOT NULL,
  verified INTEGER,
  invite  TEXT,
  email   TEXT,
  ip      TEXT,
  net     TEXT,
  geo     TEXT,
  held    TEXT
);
CREATE INDEX challenge_user ON challenge (user, made);

-- sends is how many emails went out each day (days since 1970), so the
-- Worker stays inside the plan's included emails (src/signup.ts, budget).
CREATE TABLE sends (
  day INTEGER PRIMARY KEY,
  n   INTEGER NOT NULL
);

-- ledger is every release, invite, redemption, revocation, hold and leave,
-- in order. Each entry's mac is keyed over the one before it, so an entry
-- changed, dropped or slipped in breaks every mac after it. Nothing here
-- is ever updated or deleted. actor and subject are keyed hashes of
-- Discord IDs.
CREATE TABLE ledger (
  seq     INTEGER PRIMARY KEY,
  launch  TEXT    NOT NULL,
  kind    TEXT    NOT NULL,
  actor   TEXT    NOT NULL,
  subject TEXT    NOT NULL,
  ref     TEXT    NOT NULL,
  at      INTEGER NOT NULL,
  mac     TEXT    NOT NULL
);
CREATE TRIGGER ledger_no_update BEFORE UPDATE ON ledger BEGIN SELECT RAISE(ABORT, 'the ledger is append only'); END;
CREATE TRIGGER ledger_no_delete BEFORE DELETE ON ledger BEGIN SELECT RAISE(ABORT, 'the ledger is append only'); END;

-- launch is a launch cygnet registered from its server, by name: the
-- server's id, name and picture, as cygnet read them with its own token.
-- A launch here has a page without a change to the code.
CREATE TABLE launch (
  name  TEXT    PRIMARY KEY,
  guild TEXT    NOT NULL,
  title TEXT    NOT NULL,
  icon  TEXT    NOT NULL,
  at    INTEGER NOT NULL
);
