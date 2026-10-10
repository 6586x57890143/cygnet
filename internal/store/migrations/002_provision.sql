-- The layout each server was provisioned with, and how far cygnet goes on
-- her own when it drifts from it. No row: nothing is watched.
create table provision (
	guild_id bigint primary key,
	layout   text   not null,
	mode     text   not null default 'propose' check (mode in ('watch', 'propose', 'act'))
);
