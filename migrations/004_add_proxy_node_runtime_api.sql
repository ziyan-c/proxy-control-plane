ALTER TABLE proxy_nodes
    ADD COLUMN IF NOT EXISTS runtime_api_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS runtime_api_host text,
    ADD COLUMN IF NOT EXISTS runtime_api_port bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS runtime_inbound_tag text,
    ADD COLUMN IF NOT EXISTS last_runtime_sync_at timestamptz,
    ADD COLUMN IF NOT EXISTS last_runtime_sync_error text;

CREATE INDEX IF NOT EXISTS idx_proxy_nodes_runtime_api_enabled ON proxy_nodes (runtime_api_enabled);

DO $$
BEGIN
    ALTER TABLE proxy_nodes
        ADD CONSTRAINT proxy_nodes_runtime_api_port_check CHECK (runtime_api_port = 0 OR runtime_api_port BETWEEN 1 AND 65535);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE proxy_nodes
        ADD CONSTRAINT proxy_nodes_runtime_api_required_check CHECK (
            runtime_api_enabled = false
            OR (
                runtime_api_host IS NOT NULL
                AND btrim(runtime_api_host) <> ''
                AND runtime_api_port BETWEEN 1 AND 65535
                AND runtime_inbound_tag IS NOT NULL
                AND btrim(runtime_inbound_tag) <> ''
            )
        );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
