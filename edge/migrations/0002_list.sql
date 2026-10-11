-- admins is who may see a launch's list: the server's owner and cygnet's
-- breakglass admin, Discord IDs, comma separated, as cygnet last sent them.
ALTER TABLE launch ADD COLUMN admins TEXT NOT NULL DEFAULT '';
