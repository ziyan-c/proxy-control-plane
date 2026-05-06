CREATE TABLE IF NOT EXISTS traffic_usage_daily (
    proxy_account_id text NOT NULL,
    proxy_node_id text NOT NULL,
    day date NOT NULL,
    upload_bytes bigint NOT NULL,
    download_bytes bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (proxy_account_id, proxy_node_id, day),
    CONSTRAINT traffic_usage_daily_bytes_check CHECK (upload_bytes >= 0 AND download_bytes >= 0),
    CONSTRAINT fk_traffic_usage_daily_proxy_account FOREIGN KEY (proxy_account_id) REFERENCES proxy_accounts (id) ON DELETE CASCADE,
    CONSTRAINT fk_traffic_usage_daily_proxy_node FOREIGN KEY (proxy_node_id) REFERENCES proxy_nodes (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_daily_day ON traffic_usage_daily (day);
CREATE INDEX IF NOT EXISTS idx_traffic_usage_daily_account_day ON traffic_usage_daily (proxy_account_id, day);
CREATE INDEX IF NOT EXISTS idx_traffic_usage_daily_node_day ON traffic_usage_daily (proxy_node_id, day);
