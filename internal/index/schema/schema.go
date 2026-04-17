package schema

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate brings db up to the latest embedded migration. Safe to call
// on a fresh database and idempotent on an already-migrated one.
//
// Each migration runs in its own transaction: a failed DDL rolls back
// cleanly, leaving schema_migrations at the last-good version.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`); err != nil {
		return fmt.Errorf("schema: init schema_migrations: %w", err)
	}

	applied, err := loadApplied(db)
	if err != nil {
		return err
	}

	entries, err := listMigrations()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if applied[e.version] {
			continue
		}
		if err := applyOne(db, e); err != nil {
			return err
		}
	}
	return nil
}

type migration struct {
	version int
	name    string
}

func listMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("schema: list migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func parseVersion(name string) (int, error) {
	i := strings.IndexAny(name, "_.")
	if i <= 0 {
		return 0, fmt.Errorf("schema: cannot parse version from %q", name)
	}
	v, err := strconv.Atoi(name[:i])
	if err != nil {
		return 0, fmt.Errorf("schema: cannot parse version from %q: %w", name, err)
	}
	return v, nil
}

func loadApplied(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("schema: read applied versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("schema: scan version: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

func applyOne(db *sql.DB, m migration) error {
	data, err := fs.ReadFile(migrationsFS, "migrations/"+m.name)
	if err != nil {
		return fmt.Errorf("schema: read %s: %w", m.name, err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("schema: begin %s: %w", m.name, err)
	}
	if _, err := tx.Exec(string(data)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("schema: apply %s: %w", m.name, err)
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		m.version, time.Now().Unix(),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("schema: record %s: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("schema: commit %s: %w", m.name, err)
	}
	return nil
}
