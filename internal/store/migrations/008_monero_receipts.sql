-- A Monero checkout is identified by an address, never amount change.
-- Preserve every allocation forever, including expired checkouts.
create table support_address (
    wallet text not null,
    address text primary key,
    minor bigint not null check (minor between 1 and 4294967295),
    invoice text unique references support_invoice deferrable initially deferred,
    unique (wallet, minor)
);
create index support_address_free on support_address (wallet, minor) where invoice is null;
create function support_address_kept() returns trigger language plpgsql as $$
begin
    if TG_OP = 'DELETE' then
        raise exception 'support_address allocations are never deleted';
    end if;
    if new.wallet <> old.wallet or new.address <> old.address or new.minor <> old.minor
       or (old.invoice is not null and new.invoice is distinct from old.invoice) then
        raise exception 'support_address allocations are never reassigned';
    end if;
    return new;
end
$$;
create trigger support_address_kept before update or delete on support_address
    for each row execute function support_address_kept();
drop index support_invoice_amount;
create unique index support_invoice_amount on support_invoice (rail, amount)
    where state in ('open', 'lapsed') and rail <> 'monero:XMR';
create unique index support_invoice_monero_destination on support_invoice (destination)
    where rail = 'monero:XMR';
alter table support_invoice add constraint support_invoice_monero_address
    check (rail <> 'monero:XMR' or destination <> '');

-- Several partial receipts may fund one invoice. Invoice row locking and
-- support_period's unique invoice still make granting backing atomic once.
alter table support_payment drop constraint support_payment_invoice_key;
create index support_payment_invoice on support_payment (invoice) where invoice is not null;
