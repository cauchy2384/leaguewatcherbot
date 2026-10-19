-- 0002_pidor_rolls.up.sql
--
-- Durable event/aggregate table for pidor rolls. At most one row per
-- (channel, day) is enforced at the storage layer via UNIQUE
-- (channel_id, rolled_on). The application supplies `rolled_on` as a
-- Europe/Moscow calendar date — see the consumer code and ADR-011 for
-- the rationale.
--
-- times_rolled defaults to 1 and represents how many rolls this row
-- stands for. Live rolls always use 1; historical aggregates
-- backfilled from pidors.json use distinct synthetic dates per user
-- within each channel (DATE '1970-01-01' + i days for the i-th user
-- sorted by user_id asc) so the daily constraint is never violated.
--
-- See PLAN-pidor-rolls-table.md and ADR-011.

CREATE TABLE pidor_rolls (
    id           BIGSERIAL   PRIMARY KEY,
    channel_id   TEXT        NOT NULL,
    user_id      TEXT        NOT NULL,
    user_name    TEXT        NOT NULL,
    rolled_on    DATE        NOT NULL,
    rolled_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    times_rolled INTEGER     NOT NULL DEFAULT 1
                  CHECK (times_rolled > 0),

    CONSTRAINT pidor_rolls_one_per_channel_day
        UNIQUE (channel_id, rolled_on)
);
