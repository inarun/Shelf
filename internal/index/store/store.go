package store

import (
	"database/sql"
	"fmt"
	"net/url"

	"github.com/inarun/Shelf/internal/index/schema"

	_ "modernc.org/sqlite"
)

// Store is the typed handle over the SQLite index. Construct via Open;
// Close when done. All operations that mutate multiple rows run in a
// single transaction.
type Store struct {
	db *sql.DB
}

// Open connects to the SQLite file at dbPath, enforces foreign keys,
// switches to WAL journaling, and runs schema.Migrate. The file is
// created if absent. An :memory: path is not currently supported; use a
// temp directory in tests.
func Open(dbPath string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)",
		url.PathEscape(dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dbPath, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping %s: %w", dbPath, err)
	}
	if err := schema.Migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
