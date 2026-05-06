CREATE TABLE IF NOT EXISTS subscription_aliases (
    id text PRIMARY KEY,
    customer_id text NOT NULL,
    name text NOT NULL,
    path text NOT NULL,
    path_hash text NOT NULL,
    enabled boolean NOT NULL,
    expires_at timestamptz,
    source_path text,
    source_sha256 text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    last_used_at timestamptz,
    last_used_ip text,
    last_used_user_agent text,
    CONSTRAINT fk_subscription_aliases_customer FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_subscription_aliases_customer_id ON subscription_aliases (customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_aliases_path ON subscription_aliases (path);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_aliases_path_hash ON subscription_aliases (path_hash);
CREATE INDEX IF NOT EXISTS idx_subscription_aliases_enabled ON subscription_aliases (enabled);
