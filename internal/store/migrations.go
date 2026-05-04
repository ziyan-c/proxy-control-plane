package store

import (
	"context"
	"database/sql"
	"fmt"
)

type migration struct {
	version    string
	statements []string
}

var migrations = []migration{
	{
		version: "20260504_0001_go_control_plane",
		statements: []string{
			`CREATE TABLE IF NOT EXISTS customers (
				id TEXT PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				display_name TEXT,
				status TEXT NOT NULL DEFAULT 'active',
				expires_at TIMESTAMPTZ,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`ALTER TABLE customers ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP`,
			`ALTER TABLE customers ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP`,
			`CREATE INDEX IF NOT EXISTS ix_customers_status ON customers (status)`,
			`CREATE TABLE IF NOT EXISTS proxy_nodes (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				hostname TEXT NOT NULL,
				public_host TEXT,
				region TEXT,
				protocol TEXT NOT NULL DEFAULT 'vless',
				port INTEGER NOT NULL DEFAULT 443,
				transport TEXT NOT NULL DEFAULT 'tcp',
				security TEXT NOT NULL DEFAULT 'none',
				sni TEXT,
				fingerprint TEXT,
				alpn TEXT,
				path TEXT,
				host_header TEXT,
				reality_public_key TEXT,
				reality_short_id TEXT,
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`ALTER TABLE proxy_nodes ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP`,
			`ALTER TABLE proxy_nodes ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP`,
			`CREATE INDEX IF NOT EXISTS ix_proxy_nodes_enabled ON proxy_nodes (enabled)`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS sni TEXT`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS fingerprint TEXT`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS alpn TEXT`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS path TEXT`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS host_header TEXT`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS reality_public_key TEXT`,
			`ALTER TABLE proxy_nodes ADD COLUMN IF NOT EXISTS reality_short_id TEXT`,
			`CREATE TABLE IF NOT EXISTS proxy_accounts (
				id TEXT PRIMARY KEY,
				customer_id TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
				protocol TEXT NOT NULL DEFAULT 'vless',
				uuid TEXT NOT NULL UNIQUE,
				email_tag TEXT NOT NULL,
				flow TEXT,
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				expires_at TIMESTAMPTZ,
				traffic_limit_bytes BIGINT,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`ALTER TABLE proxy_accounts ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP`,
			`ALTER TABLE proxy_accounts ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP`,
			`CREATE INDEX IF NOT EXISTS ix_proxy_accounts_customer_id ON proxy_accounts (customer_id)`,
			`CREATE INDEX IF NOT EXISTS ix_proxy_accounts_enabled ON proxy_accounts (enabled)`,
			`CREATE TABLE IF NOT EXISTS proxy_account_nodes (
				proxy_account_id TEXT NOT NULL REFERENCES proxy_accounts(id) ON DELETE CASCADE,
				proxy_node_id TEXT NOT NULL REFERENCES proxy_nodes(id) ON DELETE CASCADE,
				PRIMARY KEY (proxy_account_id, proxy_node_id)
			)`,
			`CREATE TABLE IF NOT EXISTS subscription_tokens (
				id TEXT PRIMARY KEY,
				customer_id TEXT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
				name TEXT NOT NULL DEFAULT 'default',
				token_hash TEXT NOT NULL UNIQUE,
				enabled BOOLEAN NOT NULL DEFAULT TRUE,
				expires_at TIMESTAMPTZ,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_used_at TIMESTAMPTZ,
				last_used_ip TEXT,
				last_used_user_agent TEXT
			)`,
			`ALTER TABLE subscription_tokens ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP`,
			`ALTER TABLE subscription_tokens ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP`,
			`ALTER TABLE subscription_tokens ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP`,
			`ALTER TABLE subscription_tokens ADD COLUMN IF NOT EXISTS last_used_ip TEXT`,
			`ALTER TABLE subscription_tokens ADD COLUMN IF NOT EXISTS last_used_user_agent TEXT`,
			`CREATE INDEX IF NOT EXISTS ix_subscription_tokens_customer_id ON subscription_tokens (customer_id)`,
			`CREATE INDEX IF NOT EXISTS ix_subscription_tokens_enabled ON subscription_tokens (enabled)`,
			`CREATE TABLE IF NOT EXISTS traffic_usage (
				id TEXT PRIMARY KEY,
				proxy_account_id TEXT NOT NULL REFERENCES proxy_accounts(id) ON DELETE CASCADE,
				proxy_node_id TEXT NOT NULL REFERENCES proxy_nodes(id) ON DELETE CASCADE,
				upload_bytes BIGINT NOT NULL DEFAULT 0,
				download_bytes BIGINT NOT NULL DEFAULT 0,
				recorded_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`ALTER TABLE traffic_usage ALTER COLUMN recorded_at SET DEFAULT CURRENT_TIMESTAMP`,
			`CREATE INDEX IF NOT EXISTS ix_traffic_usage_account_recorded ON traffic_usage (proxy_account_id, recorded_at)`,
			`CREATE TABLE IF NOT EXISTS audit_logs (
				id TEXT PRIMARY KEY,
				actor TEXT,
				action TEXT NOT NULL,
				metadata_json TEXT,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`ALTER TABLE audit_logs ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP`,
			`CREATE INDEX IF NOT EXISTS ix_audit_logs_created_at ON audit_logs (created_at)`,
		},
	},
}

func (s *SQLStore) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	for _, migration := range migrations {
		applied, err := migrationApplied(ctx, tx, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		for _, statement := range migration.statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migration %s failed: %w", migration.version, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, migration.version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrationApplied(ctx context.Context, tx *sql.Tx, version string) (bool, error) {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT version FROM schema_migrations WHERE version = $1`, version).Scan(&existing)
	if err == nil {
		return true, nil
	}
	if errorsIsNoRows(err) {
		return false, nil
	}
	return false, err
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}
