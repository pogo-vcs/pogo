CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE
);
CREATE INDEX user_username ON users (username);

CREATE TABLE personal_access_tokens (
    id SERIAL PRIMARY KEY,
    token BYTEA NOT NULL UNIQUE,
    user_id INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX token_token ON personal_access_tokens (token);

CREATE TABLE repositories (
    id   SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    public BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX repository_name ON repositories (name);

CREATE TABLE changes (
    id BIGSERIAL PRIMARY KEY,
    repository_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    author_id INTEGER,
    depth BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (repository_id) REFERENCES repositories (id) ON DELETE CASCADE,
    FOREIGN KEY (author_id) REFERENCES users (id) ON DELETE SET NULL,
    UNIQUE (repository_id, name)
);
CREATE INDEX change_name ON changes (repository_id, name text_pattern_ops);

CREATE TABLE change_relations (
    change_id BIGINT NOT NULL,
    parent_id BIGINT,
    FOREIGN KEY (change_id) REFERENCES changes (id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES changes (id) ON DELETE CASCADE,
    UNIQUE (change_id, parent_id)
);
CREATE INDEX change_relation_change_id ON change_relations (change_id);

CREATE TABLE files (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    executable BOOLEAN NOT NULL,
    content_hash BYTEA NOT NULL,
    conflict BOOLEAN NOT NULL,
    UNIQUE (name, executable, content_hash)
);

CREATE TABLE change_files (
    change_id BIGINT NOT NULL,
    file_id BIGINT NOT NULL,
    FOREIGN KEY (change_id) REFERENCES changes (id) ON DELETE CASCADE,
    FOREIGN KEY (file_id) REFERENCES files (id) ON DELETE CASCADE,
    UNIQUE (change_id, file_id)
);
CREATE INDEX change_file_change_id ON change_files (change_id);


CREATE TABLE repository_access (
    repository_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (repository_id) REFERENCES repositories (id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (repository_id, user_id)
);
CREATE INDEX repository_access_user ON repository_access (user_id);

CREATE TABLE bookmarks (
    id BIGSERIAL PRIMARY KEY,
    repository_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    change_id BIGINT NOT NULL,
    FOREIGN KEY (repository_id) REFERENCES repositories (id) ON DELETE CASCADE,
    FOREIGN KEY (change_id) REFERENCES changes (id) ON DELETE CASCADE,
    UNIQUE (repository_id, name)
);
CREATE INDEX bookmark_repo_name ON bookmarks (repository_id, name);
