ALTER TABLE proxy_nodes
    ADD COLUMN IF NOT EXISTS runtime text NOT NULL DEFAULT 'custom';

CREATE INDEX IF NOT EXISTS idx_proxy_nodes_runtime ON proxy_nodes (runtime);

DO $$
BEGIN
    ALTER TABLE proxy_nodes
        ADD CONSTRAINT proxy_nodes_runtime_check CHECK (runtime IN ('custom', 'xray'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
