-- What a server's admins chose to leave alone: changes a layout would make
-- that cygnet no longer makes there, each by its line in the preview.
alter table provision add column leave text[] not null default '{}';
