package schema

import (
	"database/sql"
	"errors"
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

// TestMigrate_003_RatingSchemaColumns guards the 003 migration: the new
// rating_overall/rating_dimensions/rating_has_override columns must be
// present, the legacy `rating` column dropped, and the rating index
// created. PRAGMA user_version must be 3.
func TestMigrate_003_RatingSchemaColumns(t *testing.T) {
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	cols := tableColumns(t, db, "books")
	for _, want := range []string{"rating_overall", "rating_dimensions", "rating_has_override"} {
		if _, ok := cols[want]; !ok {
			t.Errorf("books.%s missing after migrate", want)
		}
	}
	if _, ok := cols["rating"]; ok {
		t.Errorf("books.rating still present; 003 should drop it")
	}
	var idx string
	if err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name=?",
		"idx_books_rating_overall",
	).Scan(&idx); err != nil {
		t.Errorf("idx_books_rating_overall missing: %v", err)
	}
	var userVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatal(err)
	}
	if userVersion != 3 {
		t.Errorf("user_version = %d, want 3", userVersion)
	}
}

// TestMigrate_003_DataMigrationCopiesRating seeds a v2-state DB with a
// legacy INTEGER rating, runs Migrate to v3, and asserts the value is
// copied into rating_overall as a REAL with rating_has_override = 1.
func TestMigrate_003_DataMigrationCopiesRating(t *testing.T) {
	db := openTestDB(t)
	// Simulate post-v2 state: run 001 + 002 manually, then insert a row.
	entries, err := listMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.version > 2 {
			continue
		}
		if err := applyOne(db, e); err != nil {
			t.Fatal(err)
		}
	}
	// Seed a row with the legacy rating column.
	if _, err := db.Exec(`INSERT INTO books (
        filename, title, rating, size_bytes, mtime_ns, indexed_at_unix
    ) VALUES (?, ?, ?, ?, ?, ?)`,
		"Foo by Bar.md", "Foo", 4, 0, 0, 0,
	); err != nil {
		t.Fatal(err)
	}
	// Now apply 003.
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var overall sql.NullFloat64
	var hasOverride int
	if err := db.QueryRow(
		"SELECT rating_overall, rating_has_override FROM books WHERE filename = ?",
		"Foo by Bar.md",
	).Scan(&overall, &hasOverride); err != nil {
		t.Fatal(err)
	}
	if !overall.Valid || overall.Float64 != 4.0 {
		t.Errorf("rating_overall = %+v, want 4.0", overall)
	}
	if hasOverride != 1 {
		t.Errorf("rating_has_override = %d, want 1", hasOverride)
	}
}

// TestMigrate_RejectsNewerUserVersion forces the DB's user_version
// above the latest embedded migration and confirms Migrate refuses to
// touch the database. This guards against data corruption on binary
// downgrades.
func TestMigrate_RejectsNewerUserVersion(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	err := Migrate(db)
	if err == nil {
		t.Fatal("Migrate accepted newer user_version, want error")
	}
	if !errors.Is(err, ErrDatabaseNewerThanBinary) {
		t.Errorf("err = %v, want ErrDatabaseNewerThanBinary", err)
	}
	// schema_migrations must NOT have been created.
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if count != 0 {
		t.Errorf("schema_migrations table created despite downgrade guard")
	}
}

// tableColumns returns a map of column name -> type for a given table.
func tableColumns(t *testing.T, db *sql.DB, table string) map[string]string {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		out[name] = typ
	}
	return out
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
