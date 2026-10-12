-- bird is the flock as cygnet last sent it: each linked bot's name, its
-- picture (a small PNG, base64) and a version of it for caches, when it
-- first and last linked. The footer shows the birds seen in the last day.
CREATE TABLE bird (
  name TEXT PRIMARY KEY,
  png TEXT NOT NULL,
  v TEXT NOT NULL,
  first INTEGER NOT NULL,
  seen INTEGER NOT NULL
);
