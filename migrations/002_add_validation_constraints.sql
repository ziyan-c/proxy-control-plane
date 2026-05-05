DO $$
BEGIN
    ALTER TABLE proxy_nodes
        ADD CONSTRAINT proxy_nodes_port_check CHECK (port BETWEEN 1 AND 65535);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE traffic_usage
        ADD CONSTRAINT traffic_usage_bytes_check CHECK (upload_bytes >= 0 AND download_bytes >= 0);
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;
