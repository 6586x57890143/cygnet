-- cygnet owns a launch's list now (SPEC.md, Membership): its line, the
-- invites its people give and the ledger of both live in her Postgres, and
-- the Worker asks her for them. What stays here is what only a sign up
-- needs: the keyed hashes that stop one person getting on twice, and what
-- cygnet sends for a launch's page.

-- signup is one person on a launch's list as far as the Worker needs to
-- know: their keyed email, connection, network and area, kept to stop a
-- second sign up and dropped on a timer (src/signup.ts). Their place, their
-- invites and who brought them in are cygnet's.
CREATE TABLE signup (
  launch TEXT    NOT NULL,
  user   TEXT    NOT NULL,
  email  TEXT,
  ip     TEXT,
  net    TEXT,
  geo    TEXT,
  at     INTEGER NOT NULL,
  PRIMARY KEY (launch, user),
  UNIQUE (launch, email)
);
CREATE INDEX signup_ip ON signup (launch, ip);
CREATE INDEX signup_net ON signup (launch, net, geo);
INSERT INTO signup (launch, user, email, ip, net, geo, at) SELECT launch, user, email, ip, net, geo, at FROM member;

-- A launch's stage and when it first opened, as cygnet sends them with its
-- server's name and picture.
ALTER TABLE launch ADD COLUMN stage TEXT NOT NULL DEFAULT 'gathering' CHECK (stage IN ('gathering', 'closed', 'open'));
ALTER TABLE launch ADD COLUMN opens INTEGER;

DROP TABLE member;
DROP TABLE invite;
DROP TABLE ledger;
DROP TABLE gone;
DROP TABLE campaign;
