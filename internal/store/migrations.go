package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

type MigrationResult struct {
	Version string
	Name    string
	Applied bool
}

type sqlMigration struct {
	version string
	name    string
	path    string
}

type appliedMigration struct {
	name     string
	checksum string
}

func (s *Store) AutoMigrate(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(
		&domain.Customer{},
		&domain.ProxyNode{},
		&domain.ProxyAccount{},
		&domain.SubscriptionToken{},
		&domain.AuthRefreshToken{},
		&domain.TrafficUsage{},
		&domain.TrafficUsageDaily{},
		&domain.DomainAccessLog{},
		&domain.AuditLog{},
	)
}

func (s *Store) ApplySQLMigrations(ctx context.Context, dir string) ([]MigrationResult, error) {
	migrations, err := loadSQLMigrations(dir)
	if err != nil {
		return nil, err
	}

	db, err := s.db.DB()
	if err != nil {
		return nil, err
	}
	if err := ensureSchemaMigrations(ctx, db); err != nil {
		return nil, err
	}

	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return nil, err
	}

	results := make([]MigrationResult, 0, len(migrations))
	for _, migration := range migrations {
		result := MigrationResult{
			Version: migration.version,
			Name:    migration.name,
		}
		content, err := os.ReadFile(migration.path)
		if err != nil {
			return results, fmt.Errorf("read migration %s: %w", migration.name, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			return results, fmt.Errorf("migration %s is empty", migration.name)
		}
		checksum := sqlChecksum(content)

		if existing, ok := applied[migration.version]; ok {
			if existing.checksum != checksum {
				return results, fmt.Errorf("migration %s was already applied with a different checksum", migration.name)
			}
			results = append(results, result)
			continue
		}

		if err := applySQLMigration(ctx, db, migration, string(content), checksum); err != nil {
			return results, err
		}
		result.Applied = true
		results = append(results, result)
	}

	return results, nil
}

func loadSQLMigrations(dir string) ([]sqlMigration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	migrations := make([]sqlMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		if version == "" {
			continue
		}
		migrations = append(migrations, sqlMigration{
			version: version,
			name:    entry.Name(),
			path:    filepath.Join(dir, entry.Name()),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].name < migrations[j].name
	})
	return migrations, nil
}

func ensureSchemaMigrations(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    name text NOT NULL,
    checksum text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`)
	return err
}

func appliedMigrations(ctx context.Context, db *sql.DB) (map[string]appliedMigration, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	migrations := map[string]appliedMigration{}
	for rows.Next() {
		var version, name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			return nil, err
		}
		migrations[version] = appliedMigration{
			name:     name,
			checksum: checksum,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return migrations, nil
}

func applySQLMigration(ctx context.Context, db *sql.DB, migration sqlMigration, content string, checksum string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, content); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
		migration.version,
		migration.name,
		checksum,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.name, err)
	}
	return nil
}

func sqlChecksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
