CREATE TABLE IF NOT EXISTS domain_access_logs (
    id text PRIMARY KEY,
    proxy_account_id text NOT NULL,
    proxy_node_id text NOT NULL,
    domain text NOT NULL,
    event_count bigint NOT NULL DEFAULT 1,
    upload_bytes bigint NOT NULL DEFAULT 0,
    download_bytes bigint NOT NULL DEFAULT 0,
    accessed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT domain_access_logs_domain_check CHECK (btrim(domain) <> ''),
    CONSTRAINT domain_access_logs_counts_check CHECK (event_count > 0),
    CONSTRAINT domain_access_logs_bytes_check CHECK (upload_bytes >= 0 AND download_bytes >= 0),
    CONSTRAINT fk_domain_access_logs_proxy_account FOREIGN KEY (proxy_account_id) REFERENCES proxy_accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_domain_access_logs_proxy_node FOREIGN KEY (proxy_node_id) REFERENCES proxy_nodes (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_domain_access_logs_accessed_at ON domain_access_logs (accessed_at);
CREATE INDEX IF NOT EXISTS idx_domain_access_logs_account_accessed ON domain_access_logs (proxy_account_id, accessed_at);
CREATE INDEX IF NOT EXISTS idx_domain_access_logs_node_accessed ON domain_access_logs (proxy_node_id, accessed_at);
CREATE INDEX IF NOT EXISTS idx_domain_access_logs_domain_accessed ON domain_access_logs (domain, accessed_at);
