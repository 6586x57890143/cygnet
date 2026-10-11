-- One row a person on a launch's list. user is their Discord ID, which is
-- what the line is kept by. email, ip, net and geo are keyed hashes, never
-- the values, kept only to stop one person signing up twice, and dropped
-- on a timer (src/signup.ts, forget).
CREATE TABLE signup (
  launch  TEXT    NOT NULL,
  place   INTEGER NOT NULL,
  user    TEXT    NOT NULL,
  email   TEXT,
  ip      TEXT,
  net     TEXT,
  geo     TEXT,
  invites INTEGER NOT NULL,
  -- held is why this person's invites wait for an admin, or NULL.
  held    TEXT,
  at      INTEGER NOT NULL,
  PRIMARY KEY (launch, user),
  UNIQUE (launch, email),
  UNIQUE (launch, place)
);

CREATE INDEX signup_ip ON signup (launch, ip);
CREATE INDEX signup_net ON signup (launch, net, geo);
