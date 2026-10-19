-- 0001_schema_migrations.down.sql
--
-- Rolls back 0001. Drops the bookkeeping table; subsequent migrations
-- (if any) must already have been rolled back first.

DROP TABLE IF EXISTS schema_migrations;
