CREATE TABLE IF NOT EXISTS ci_runs (
    id SERIAL PRIMARY KEY,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    config_filename TEXT NOT NULL,
    event_type TEXT NOT NULL,
    rev TEXT NOT NULL,
    pattern TEXT,
    reason TEXT NOT NULL,
    task_type TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    success BOOLEAN NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL,
    finished_at TIMESTAMP WITH TIME ZONE NOT NULL,
    log TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS ci_runs_repository_id_started_at_idx
    ON ci_runs (repository_id, started_at DESC);