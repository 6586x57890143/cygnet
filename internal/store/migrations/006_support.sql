-- Supporters (docs/support.md): people who back the birds for a while, paid
-- in crypto to wallets only their owner holds. cygnet holds no key and moves
-- no money: she quotes an amount that says which checkout it is, watches the
-- chains for it and records what it bought.

-- support_plan is each length on offer, and what it costs in months at the
-- monthly rate, in hundredths: a year is 1000, ten months' worth.
create table support_plan (
	months  int  primary key check (months between 1 and 36),
	worth   int  not null check (worth between 1 and 3600),
	offered bool not null default true
);
insert into support_plan (months, worth) values (1, 100), (3, 280), (6, 540), (12, 1000);

-- support_rate is each monthly amount on offer, in US cents. The lowest one
-- offered is the plain supporter; anything above it is paying more, which
-- wears the gold badge and buys nothing else.
create table support_rate (
	cents   int  primary key check (cents between 100 and 100000),
	offered bool not null default true
);
insert into support_rate (cents) values (500), (1000), (2000);

-- support_invoice is one checkout: who, what it buys, and the exact amount
-- that pays it on one rail (a token on a chain), in the token's smallest
-- unit. The amount is the price plus a few units of change that no other
-- live checkout on that rail has, which is how a transfer is matched to it:
-- a chain carries no reference on most rails, and a reference typed into a
-- wallet is a step people skip. quote is the USD price a volatile asset was
-- quoted at. A checkout is open until it expires, then lapsed, which still
-- takes a late payment and keeps its amount out of anyone else's hands, and
-- void once that grace has passed.
create table support_invoice (
	id      text        primary key check (id ~ '^[a-z2-7]{26}$'),
	user_id bigint      not null,
	months  int         not null check (months > 0),
	cents   int         not null check (cents > 0),
	price   int         not null check (price > 0),
	gold    bool        not null,
	rail    text        not null,
	amount  numeric(78) not null check (amount > 0),
	quote   text,
	made    timestamptz not null,
	expires timestamptz not null check (expires > made),
	state   text        not null default 'open' check (state in ('open', 'lapsed', 'paid', 'void')),
	paid    timestamptz,
	check ((state = 'paid') = (paid is not null))
);
-- One live checkout per amount on a rail: what makes a transfer's amount
-- name the checkout it pays.
create unique index support_invoice_amount on support_invoice (rail, amount) where state in ('open', 'lapsed');
create index support_invoice_user on support_invoice (user_id, made desc);

-- support_payment is every transfer cygnet saw arrive on a rail, matched to
-- a checkout or not: one that matched nothing (a wrong amount, a gift) is
-- for the owner to sort out by hand. A payment is a fact about a chain, so
-- once written it is never changed or deleted.
create table support_payment (
	rail    text        not null,
	tx      text        not null,
	idx     int         not null default 0,
	amount  numeric(78) not null check (amount > 0),
	sender  text        not null default '',
	seen    timestamptz not null,
	invoice text        unique references support_invoice,
	primary key (rail, tx, idx)
);
create index support_payment_unmatched on support_payment (seen desc) where invoice is null;

create function support_payment_kept() returns trigger language plpgsql as $$
begin
	raise exception 'support_payment is a record of what arrived: it is never changed or deleted';
end
$$;
create trigger support_payment_kept before update or delete on support_payment
	for each row execute function support_payment_kept();

-- support_period is what someone has backed: from starts to ends, each
-- after the last one they had, so paying again before it runs out adds on
-- rather than overlapping. A checkout paid it, or the owner gave it (gift is
-- who gave it).
create table support_period (
	id      bigserial   primary key,
	user_id bigint      not null,
	starts  timestamptz not null,
	ends    timestamptz not null check (ends > starts),
	gold    bool        not null,
	invoice text        unique references support_invoice,
	gift    bigint,
	made    timestamptz not null,
	check ((invoice is null) <> (gift is null))
);
create index support_period_user on support_period (user_id, ends desc);

-- support_cursor is where reading each watched account resumes: a block, a
-- time or a signature, as its chain counts. since is when it was first
-- read; nothing older is ever read, so a wallet's history before the first
-- start is not taken for payments. read and err are the last read and why
-- the last one failed, for the panel.
create table support_cursor (
	key   text        primary key,
	at    text        not null default '',
	since timestamptz not null,
	read  timestamptz,
	err   text        not null default ''
);
