CREATE TABLE secrets (
    repository_id INTEGER NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    FOREIGN KEY (repository_id) REFERENCES repositories (id) ON DELETE CASCADE,
    PRIMARY KEY (repository_id, key)
);
CREATE INDEX secret_repository ON secrets (repository_id);