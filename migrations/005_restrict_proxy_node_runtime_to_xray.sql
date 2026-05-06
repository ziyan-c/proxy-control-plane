UPDATE proxy_nodes
SET runtime = 'xray'
WHERE runtime = 'v2ray';

ALTER TABLE proxy_nodes
    DROP CONSTRAINT IF EXISTS proxy_nodes_runtime_check;

ALTER TABLE proxy_nodes
    ADD CONSTRAINT proxy_nodes_runtime_check CHECK (runtime IN ('custom', 'xray'));
