DELETE FROM ci_runs;

ALTER TABLE ci_runs
    ALTER COLUMN log TYPE BYTEA USING log::bytea,
    ALTER COLUMN finished_at DROP NOT NULL;