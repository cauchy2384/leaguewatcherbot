-- 0001_schema_migrations.up.sql
--
-- Creates the bookkeeping table used by tools/migrator to track which
-- schema migrations have been applied. This is the meta-migration: the
-- runner inserts the row for version 1 itself (within the same
-- transaction that runs this DDL) once this table exists.
--
-- Idempotency: PRIMARY KEY on `version` prevents accidental
-- double-application of any later migration.

CREATE TABLE schema_migrations (
    version    INTEGER     PRIMARY KEY,
    name       TEXT        NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
