package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSQLMigrationsSortsSQLFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"002_add_indexes.sql": "SELECT 2;",
		"001_init.sql":        "SELECT 1;",
		"README.md":           "ignore me",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	migrations, err := loadSQLMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(migrations))
	}

	if migrations[0].name != "001_init.sql" || migrations[0].version != "001_init" {
		t.Fatalf("unexpected first migration: %#v", migrations[0])
	}
	if migrations[1].name != "002_add_indexes.sql" || migrations[1].version != "002_add_indexes" {
		t.Fatalf("unexpected second migration: %#v", migrations[1])
	}
}
