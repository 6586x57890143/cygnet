-- Launches (SPEC.md, What a member sees): the line people wait in for a
-- server to open, the invites they give, the waves that let them in, and
-- the ledger of all of it. Postgres owns these; cygnet.lol keeps only what
-- a sign up needs to stop one person getting in twice.

-- launch is a server's campaign, by the name of its page at cygnet.lol.
-- opens is when it first opened, which starts the clock on deleting its
-- list. ceiling is the joins a minute the valve keeps under, and paused
-- stops it between waves.
create table launch (
	name     text        primary key check (name ~ '^[a-z0-9-]{2,32}$'),
	guild_id bigint      not null unique,
	stage    text        not null default 'gathering' check (stage in ('gathering', 'closed', 'open')),
	opens    timestamptz,
	ceiling  int         not null default 10 check (ceiling between 1 and 1000),
	paused   bool        not null default false,
	made     timestamptz not null
);

-- wave is one lot the valve let in, and why it was that size, in words.
-- settled is when the next one could follow.
create table wave (
	id      bigserial   primary key,
	launch  text        not null references launch,
	size    int         not null check (size > 0),
	opened  timestamptz not null,
	settled timestamptz,
	why     text        not null
);

-- A launch's invites are codes into its server, spent onto its line and
-- arrived by at the spender's wave.
alter table code add column launch text references launch;

-- line is everyone waiting for a launch: in place order, by a code someone
-- gave them (code) or on their own. held is why their invites wait for an
-- admin. released is how many invites they may give. sealed is their
-- Discord sign in, encrypted to cygnet, for adding them at their wave.
-- wave and admitted say when they were let in.
create table line (
	launch   text        not null references launch,
	user_id  bigint      not null,
	place    int         not null,
	code     bytea       references code,
	held     text,
	released int         not null default 0 check (released between 0 and 10),
	sealed   bytea,
	wave     bigint      references wave,
	admitted timestamptz,
	at       timestamptz not null,
	primary key (launch, user_id),
	unique (launch, place),
	check ((wave is null) = (admitted is null))
);

-- A join is caused by a code or by a wave, never both.
alter table joined add column wave bigint references wave;
alter table joined add check (code is null or wave is null);

-- gone is each launch whose list was deleted when its time ran out: the one
-- way its ledger can go.
create table gone (
	launch text        primary key,
	at     timestamptz not null
);

-- ledger is every change to a launch's line and invites, in order. Each
-- entry's mac is keyed over the one before it in its launch, so an entry
-- changed, dropped or slipped in breaks every mac after it. actor and
-- subject are keyed hashes of Discord IDs, empty for cygnet herself.
create table ledger (
	seq     bigserial   primary key,
	launch  text        not null,
	kind    text        not null,
	actor   bytea       not null,
	subject bytea       not null,
	ref     text        not null,
	at      timestamptz not null,
	mac     bytea       not null
);
create index ledger_launch on ledger (launch, seq);

create function ledger_append_only() returns trigger language plpgsql as $$
begin
	if tg_op = 'DELETE' and exists (select 1 from gone where launch = old.launch) then
		return old;
	end if;
	raise exception 'the ledger is append only';
end
$$;
create trigger ledger_append_only before update or delete on ledger
	for each row execute function ledger_append_only();
