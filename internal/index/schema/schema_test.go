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
	// schema_migrations must contain exactly one row per embedded
	// migration file, and the highest version must match the highest
	// file present in migrations/.
	want, err := listMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatalf("no migrations found on disk — listMigrations returned empty")
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(want) {
		t.Errorf("applied migrations = %d, want %d", n, len(want))
	}
	var latest int
	if err := db.QueryRow("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&latest); err != nil {
		t.Fatal(err)
	}
	if latest != want[len(want)-1].version {
		t.Errorf("latest applied = %d, want %d", latest, want[len(want)-1].version)
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
	// Capture count after first pass; second pass must not add rows.
	var before int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate should be no-op: %v", err)
	}
	var after int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("idempotence violated: %d → %d rows", before, after)
	}
}

// TestMigrate_002_BookCategoriesCategoryIndex guards the 002 migration:
// the index on book_categories.category_id must exist after Migrate so
// future category filters don't fall to a table scan.
func TestMigrate_002_BookCategoriesCategoryIndex(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var name string
	err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name=?",
		"idx_book_categories_category_id",
	).Scan(&name)
	if err != nil {
		t.Fatalf("idx_book_categories_category_id missing: %v", err)
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
