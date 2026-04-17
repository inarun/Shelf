package schema

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// openTestDB opens a file-backed database under t.TempDir with foreign
// keys on. A file-backed DB (rather than :memory:) matches the shape of
// the real store and keeps the sqlite driver's behavior consistent.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrate_FreshDB_AppliesAll(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	// schema_migrations has version 1.
	var v int
	if err := db.QueryRow("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Errorf("expected latest version 1, got %d", v)
	}
	// All expected tables present.
	for _, table := range []string{
		"books", "authors", "categories", "series",
		"book_authors", "book_categories", "schema_migrations",
	} {
		var count int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("table %s missing", table)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate should be no-op: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("duplicate migration rows: %d", n)
	}
}

func TestMigrate_ForeignKeysEnforced(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var on int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Fatalf("foreign_keys pragma should be 1, got %d", on)
	}
	// Inserting a book_authors row referencing a non-existent book must
	// fail — confirms the FK constraint is live, not just declared.
	_, err := db.Exec("INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?)", 999, 999, 0)
	if err == nil {
		t.Error("expected FK violation, got nil")
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want int
		bad  bool
	}{
		{"001_initial.sql", 1, false},
		{"042_add_col.sql", 42, false},
		{"1.sql", 1, false},
		{"not-numbered.sql", 0, true},
		{"abc_foo.sql", 0, true},
		{"_.sql", 0, true},
	}
	for _, c := range cases {
		got, err := parseVersion(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("parseVersion(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseVersion(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseVersion(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
