CREATE TABLE invites (
    id SERIAL PRIMARY KEY,
    token BYTEA NOT NULL UNIQUE,
    created_by_user_id INTEGER NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    used_at TIMESTAMP WITH TIME ZONE DEFAULT NULL,
    used_by_user_id INTEGER DEFAULT NULL,
    FOREIGN KEY (created_by_user_id) REFERENCES users (id) ON DELETE CASCADE,
    FOREIGN KEY (used_by_user_id) REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX invite_token ON invites (token);
CREATE INDEX invite_created_by ON invites (created_by_user_id);
CREATE INDEX invite_expires_at ON invites (expires_at);