-- Codes, and who came in by them (SPEC.md, Membership). Postgres owns these:
-- the Worker at cygnet.lol only asks.

-- code is one link a member made for their server: hash is the SHA-256 of
-- the code, which only the link holds. Spent once, by spent_by, or taken
-- back, never both. arrived is when spent_by joined the server through it.
create table code (
	hash     bytea       primary key check (length(hash) = 32),
	guild_id bigint      not null,
	owner_id bigint      not null,
	made     timestamptz not null,
	expires  timestamptz not null,
	spent_by bigint,
	spent_at timestamptz,
	revoked  timestamptz,
	arrived  timestamptz,
	check (spent_by is null or revoked is null),
	check ((spent_by is null) = (spent_at is null)),
	check (arrived is null or spent_by is not null)
);
create index code_owner on code (guild_id, owner_id, made desc);
-- Someone holds one way into a server at a time: one code they spent there
-- and haven't arrived by.
create unique index code_pending on code (guild_id, spent_by) where spent_by is not null and arrived is null;

-- joined is everyone cygnet saw join a server, and why: by a code (code and
-- its owner, sponsor) or, with both null, a join she cannot trace. left_at
-- is set when they leave; a rejoin replaces the row. Members who were there
-- before cygnet watched have none.
create table joined (
	guild_id bigint      not null,
	user_id  bigint      not null,
	at       timestamptz not null,
	code     bytea       references code,
	sponsor  bigint,
	left_at  timestamptz,
	primary key (guild_id, user_id),
	check ((code is null) = (sponsor is null))
);

-- invites is how many links each member of a server may have out or used
-- at once, as its admins set it. No row: members make none.
create table invites (
	guild_id bigint primary key,
	each     int    not null check (each between 0 and 10)
);
