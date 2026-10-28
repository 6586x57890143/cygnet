-- Preserve the destination quoted to each person. Empty is a legacy
-- checkout, whose destination was supplied by the process configuration.
alter table support_invoice add column destination text not null default '';

-- Indexed transfers have stable event identities, including several
-- contract sends in one transaction. Existing readers keep event empty
-- and retain exactly their old (rail, tx, idx) identity.
alter table support_payment add column event text not null default '';
alter table support_payment drop constraint support_payment_pkey;
alter table support_payment add primary key (rail, tx, idx, event);
