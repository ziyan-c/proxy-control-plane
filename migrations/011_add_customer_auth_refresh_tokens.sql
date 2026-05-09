ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS password_hash text;

ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS session_epoch text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
    id text PRIMARY KEY,
    principal_type text NOT NULL,
    customer_id text,
    subject text NOT NULL,
    session_version text NOT NULL,
    token_hash text NOT NULL,
    enabled boolean NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    last_used_at timestamptz,
    last_used_ip text,
    last_used_user_agent text,
    revoked_at timestamptz,
    replaced_by_id text,
    CONSTRAINT chk_auth_refresh_tokens_principal_type CHECK (principal_type IN ('admin', 'customer')),
    CONSTRAINT chk_auth_refresh_tokens_principal_ref CHECK (
        (principal_type = 'admin' AND customer_id IS NULL)
        OR (principal_type = 'customer' AND customer_id IS NOT NULL)
    ),
    CONSTRAINT fk_auth_refresh_tokens_customer FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_refresh_tokens_token_hash ON auth_refresh_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_customer_id ON auth_refresh_tokens (customer_id);
CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_enabled ON auth_refresh_tokens (enabled);
CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_expires_at ON auth_refresh_tokens (expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_revoked_at ON auth_refresh_tokens (revoked_at);
