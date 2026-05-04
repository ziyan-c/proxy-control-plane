CREATE TABLE IF NOT EXISTS customers (
    id text PRIMARY KEY,
    email text NOT NULL,
    display_name text,
    status text NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_email ON customers (email);
CREATE INDEX IF NOT EXISTS idx_customers_status ON customers (status);

CREATE TABLE IF NOT EXISTS proxy_nodes (
    id text PRIMARY KEY,
    name text NOT NULL,
    hostname text NOT NULL,
    public_host text,
    region text,
    protocol text NOT NULL,
    port bigint NOT NULL,
    transport text NOT NULL,
    security text NOT NULL,
    sni text,
    fingerprint text,
    alpn text,
    path text,
    host_header text,
    reality_public_key text,
    reality_short_id text,
    enabled boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_nodes_name ON proxy_nodes (name);
CREATE INDEX IF NOT EXISTS idx_proxy_nodes_enabled ON proxy_nodes (enabled);

CREATE TABLE IF NOT EXISTS proxy_accounts (
    id text PRIMARY KEY,
    customer_id text NOT NULL,
    protocol text NOT NULL,
    uuid text NOT NULL,
    email_tag text NOT NULL,
    flow text,
    enabled boolean NOT NULL,
    expires_at timestamptz,
    traffic_limit_bytes bigint,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT fk_proxy_accounts_customer FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_proxy_accounts_customer_id ON proxy_accounts (customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_accounts_uuid ON proxy_accounts (uuid);
CREATE INDEX IF NOT EXISTS idx_proxy_accounts_enabled ON proxy_accounts (enabled);

CREATE TABLE IF NOT EXISTS proxy_account_nodes (
    proxy_account_id text NOT NULL,
    proxy_node_id text NOT NULL,
    PRIMARY KEY (proxy_account_id, proxy_node_id),
    CONSTRAINT fk_proxy_account_nodes_proxy_account FOREIGN KEY (proxy_account_id) REFERENCES proxy_accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_proxy_account_nodes_proxy_node FOREIGN KEY (proxy_node_id) REFERENCES proxy_nodes (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_proxy_account_nodes_proxy_node_id ON proxy_account_nodes (proxy_node_id);

CREATE TABLE IF NOT EXISTS subscription_tokens (
    id text PRIMARY KEY,
    customer_id text NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    enabled boolean NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    last_used_at timestamptz,
    last_used_ip text,
    last_used_user_agent text,
    CONSTRAINT fk_subscription_tokens_customer FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_subscription_tokens_customer_id ON subscription_tokens (customer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_tokens_token_hash ON subscription_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_subscription_tokens_enabled ON subscription_tokens (enabled);

CREATE TABLE IF NOT EXISTS traffic_usage (
    id text PRIMARY KEY,
    proxy_account_id text NOT NULL,
    proxy_node_id text NOT NULL,
    upload_bytes bigint NOT NULL,
    download_bytes bigint NOT NULL,
    recorded_at timestamptz NOT NULL,
    CONSTRAINT fk_traffic_usage_proxy_account FOREIGN KEY (proxy_account_id) REFERENCES proxy_accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_traffic_usage_proxy_node FOREIGN KEY (proxy_node_id) REFERENCES proxy_nodes (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS ix_traffic_usage_account_recorded ON traffic_usage (proxy_account_id, recorded_at);
CREATE INDEX IF NOT EXISTS idx_traffic_usage_proxy_node_id ON traffic_usage (proxy_node_id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id text PRIMARY KEY,
    actor text,
    action text NOT NULL,
    metadata_json text,
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at);
