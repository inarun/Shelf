package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// BookRow is the flat row representation used by callers. Pointer-typed
// fields (TotalPages, Rating, SeriesID, SeriesIndex) distinguish "not
// set" from zero. StartedDates/FinishedDates are ISO-formatted strings
// matching the vault frontmatter format.
type BookRow struct {
	ID            int64
	Filename      string
	CanonicalName bool
	Title         string
	Subtitle      string
	Publisher     string
	PublishDate   string
	TotalPages    *int64
	ISBN          string
	Cover         string
	Format        string
	Source        string
	Rating        *int64
	Status        string
	ReadCount     int64
	SeriesID      *int64
	SeriesName    string
	SeriesIndex   *float64
	Authors       []string
	Categories    []string
	StartedDates  []string
	FinishedDates []string
	SizeBytes     int64
	MtimeNanos    int64
	IndexedAtUnix int64
	Warnings      []string
}

// FileStat holds the staleness pair used by the sync reconciler to skip
// re-parsing files whose on-disk stat matches what's already in the
// index.
type FileStat struct {
	SizeBytes  int64
	MtimeNanos int64
}

// Filter is the query filter for ListBooks. Empty-string / nil fields
// mean "any".
type Filter struct {
	Status        string
	SeriesID      *int64
	AuthorName    string
	CanonicalOnly bool
}

// ErrNotFound is returned by Get lookups when no row matches.
var ErrNotFound = errors.New("store: not found")

// UpsertBook inserts or updates the row identified by Filename. Returns
// the row's id. Authors, categories, and series rows are upserted by
// name (case-insensitive). Join-table rows are rebuilt from scratch on
// each call so removed authors/categories disappear cleanly.
func (s *Store) UpsertBook(ctx context.Context, row BookRow) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var seriesID *int64
	if row.SeriesName != "" {
		id, err := upsertNamed(ctx, tx, "series", row.SeriesName)
		if err != nil {
			return 0, err
		}
		seriesID = &id
	}

	startedJSON, err := json.Marshal(normalizeStringSlice(row.StartedDates))
	if err != nil {
		return 0, fmt.Errorf("store: marshal started: %w", err)
	}
	finishedJSON, err := json.Marshal(normalizeStringSlice(row.FinishedDates))
	if err != nil {
		return 0, fmt.Errorf("store: marshal finished: %w", err)
	}
	warningsJSON, err := json.Marshal(normalizeStringSlice(row.Warnings))
	if err != nil {
		return 0, fmt.Errorf("store: marshal warnings: %w", err)
	}

	canonicalInt := 0
	if row.CanonicalName {
		canonicalInt = 1
	}

	// One atomic UPSERT on the books row keyed by the UNIQUE filename.
	// RETURNING id works on SQLite 3.35+ which modernc.org/sqlite bundles.
	const upsertSQL = `
INSERT INTO books (
    filename, canonical_name, title, subtitle, publisher, publish_date,
    total_pages, isbn, cover, format, source, rating, status, read_count,
    series_id, series_index, started_json, finished_json,
    size_bytes, mtime_ns, indexed_at_unix, warnings_json
) VALUES (
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?, ?, ?
)
ON CONFLICT(filename) DO UPDATE SET
    canonical_name  = excluded.canonical_name,
    title           = excluded.title,
    subtitle        = excluded.subtitle,
    publisher       = excluded.publisher,
    publish_date    = excluded.publish_date,
    total_pages     = excluded.total_pages,
    isbn            = excluded.isbn,
    cover           = excluded.cover,
    format          = excluded.format,
    source          = excluded.source,
    rating          = excluded.rating,
    status          = excluded.status,
    read_count      = excluded.read_count,
    series_id       = excluded.series_id,
    series_index    = excluded.series_index,
    started_json    = excluded.started_json,
    finished_json   = excluded.finished_json,
    size_bytes      = excluded.size_bytes,
    mtime_ns        = excluded.mtime_ns,
    indexed_at_unix = excluded.indexed_at_unix,
    warnings_json   = excluded.warnings_json
RETURNING id
`
	var bookID int64
	if err := tx.QueryRowContext(ctx, upsertSQL,
		row.Filename, canonicalInt, row.Title, row.Subtitle, row.Publisher, row.PublishDate,
		nullableInt64(row.TotalPages), row.ISBN, row.Cover, row.Format, row.Source,
		nullableInt64(row.Rating), row.Status, row.ReadCount,
		nullableInt64(seriesID), nullableFloat64(row.SeriesIndex),
		string(startedJSON), string(finishedJSON),
		row.SizeBytes, row.MtimeNanos, row.IndexedAtUnix, string(warningsJSON),
	).Scan(&bookID); err != nil {
		return 0, fmt.Errorf("store: upsert book %s: %w", row.Filename, err)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM book_authors WHERE book_id = ?", bookID); err != nil {
		return 0, fmt.Errorf("store: clear authors: %w", err)
	}
	for i, name := range row.Authors {
		if name == "" {
			continue
		}
		authorID, err := upsertNamed(ctx, tx, "authors", name)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?)",
			bookID, authorID, i,
		); err != nil {
			return 0, fmt.Errorf("store: insert book_author: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM book_categories WHERE book_id = ?", bookID); err != nil {
		return 0, fmt.Errorf("store: clear categories: %w", err)
	}
	for _, name := range row.Categories {
		if name == "" {
			continue
		}
		categoryID, err := upsertNamed(ctx, tx, "categories", name)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO book_categories (book_id, category_id) VALUES (?, ?)",
			bookID, categoryID,
		); err != nil {
			return 0, fmt.Errorf("store: insert book_category: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit upsert: %w", err)
	}
	return bookID, nil
}

// upsertNamed inserts a row into (series|authors|categories) by unique
// name (case-insensitive via the column COLLATE NOCASE) and returns the
// id. Works for pre-existing rows too.
//
// The table argument must be one of the three known table names; it is
// never derived from user input. Using a compile-time allowlist plus a
// panic on mismatch keeps static analysis happy and fails fast if a
// future caller typos the argument.
func upsertNamed(ctx context.Context, tx *sql.Tx, table, name string) (int64, error) {
	stmt, ok := upsertStatements[table]
	if !ok {
		return 0, fmt.Errorf("store: upsertNamed called with unknown table %q", table)
	}
	var id int64
	if err := tx.QueryRowContext(ctx, stmt, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: upsert %s %q: %w", table, name, err)
	}
	return id, nil
}

// upsertStatements pre-builds the three legal statements used by
// upsertNamed. All SQL here is static — no user input ever reaches the
// statement text.
var upsertStatements = map[string]string{
	"series":     "INSERT INTO series (name) VALUES (?) ON CONFLICT(name) DO UPDATE SET name = excluded.name RETURNING id",
	"authors":    "INSERT INTO authors (name) VALUES (?) ON CONFLICT(name) DO UPDATE SET name = excluded.name RETURNING id",
	"categories": "INSERT INTO categories (name) VALUES (?) ON CONFLICT(name) DO UPDATE SET name = excluded.name RETURNING id",
}

// GetBookByFilename returns the full row for a book, including joined
// authors, categories, and series name. Returns ErrNotFound if no row
// has the given filename.
func (s *Store) GetBookByFilename(ctx context.Context, filename string) (*BookRow, error) {
	row := s.db.QueryRowContext(ctx, bookSelectByFilename, filename)
	br, err := scanBook(row)
	if err != nil {
		return nil, err
	}
	if err := s.loadJoined(ctx, br); err != nil {
		return nil, err
	}
	return br, nil
}

const bookSelectByFilename = `
SELECT
    b.id, b.filename, b.canonical_name, b.title, b.subtitle, b.publisher,
    b.publish_date, b.total_pages, b.isbn, b.cover, b.format, b.source,
    b.rating, b.status, b.read_count, b.series_id, b.series_index,
    b.started_json, b.finished_json, b.size_bytes, b.mtime_ns,
    b.indexed_at_unix, b.warnings_json,
    COALESCE(s.name, '')
FROM books b
LEFT JOIN series s ON s.id = b.series_id
WHERE b.filename = ?
`

// ErrAmbiguousISBN is returned by GetBookByISBN when more than one row
// matches the queried ISBN. The caller should treat this as "cannot
// decide automatically" and surface a conflict for user resolution —
// Goodreads import specifically flags ambiguous-ISBN records as
// conflicts rather than silently picking a row.
var ErrAmbiguousISBN = errors.New("store: multiple books with same ISBN")

// GetBookByISBN returns the book row whose ISBN column exactly equals
// isbn (after caller normalization — the store does not strip hyphens or
// whitespace). The empty string never matches any row.
//
// If two or more rows share the same ISBN, the function returns
// ErrAmbiguousISBN — the caller (e.g., the Goodreads importer) should
// treat that as a conflict. Returns ErrNotFound when no row matches.
func (s *Store) GetBookByISBN(ctx context.Context, isbn string) (*BookRow, error) {
	if isbn == "" {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, bookSelectByISBN, isbn)
	if err != nil {
		return nil, fmt.Errorf("store: query by isbn: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var first *BookRow
	for rows.Next() {
		br, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		if first != nil {
			return nil, ErrAmbiguousISBN
		}
		first = br
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: isbn rows: %w", err)
	}
	if first == nil {
		return nil, ErrNotFound
	}
	if err := s.loadJoined(ctx, first); err != nil {
		return nil, err
	}
	return first, nil
}

const bookSelectByISBN = `
SELECT
    b.id, b.filename, b.canonical_name, b.title, b.subtitle, b.publisher,
    b.publish_date, b.total_pages, b.isbn, b.cover, b.format, b.source,
    b.rating, b.status, b.read_count, b.series_id, b.series_index,
    b.started_json, b.finished_json, b.size_bytes, b.mtime_ns,
    b.indexed_at_unix, b.warnings_json,
    COALESCE(s.name, '')
FROM books b
LEFT JOIN series s ON s.id = b.series_id
WHERE b.isbn = ? AND b.isbn != ''
LIMIT 2
`

// DeleteBookByFilename removes a book. ON DELETE CASCADE tears down the
// join-table rows; series/authors/categories rows survive, which is
// intentional (users may want to ask "has Shelf ever seen this author"
// even after the last book by them disappears).
func (s *Store) DeleteBookByFilename(ctx context.Context, filename string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM books WHERE filename = ?", filename)
	if err != nil {
		return fmt.Errorf("store: delete %s: %w", filename, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete rows-affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListBooks returns books matching the filter, ordered by title. Empty
// filter returns everything. An AuthorName filter does an exact
// case-insensitive match through the join table.
func (s *Store) ListBooks(ctx context.Context, f Filter) ([]BookRow, error) {
	sqlStmt := `SELECT
    b.id, b.filename, b.canonical_name, b.title, b.subtitle, b.publisher,
    b.publish_date, b.total_pages, b.isbn, b.cover, b.format, b.source,
    b.rating, b.status, b.read_count, b.series_id, b.series_index,
    b.started_json, b.finished_json, b.size_bytes, b.mtime_ns,
    b.indexed_at_unix, b.warnings_json,
    COALESCE(s.name, '')
FROM books b
LEFT JOIN series s ON s.id = b.series_id`
	args := []any{}
	where := []string{}
	if f.Status != "" {
		where = append(where, "b.status = ?")
		args = append(args, f.Status)
	}
	if f.SeriesID != nil {
		where = append(where, "b.series_id = ?")
		args = append(args, *f.SeriesID)
	}
	if f.CanonicalOnly {
		where = append(where, "b.canonical_name = 1")
	}
	if f.AuthorName != "" {
		where = append(where, `b.id IN (
    SELECT ba.book_id FROM book_authors ba
    JOIN authors a ON a.id = ba.author_id
    WHERE a.name = ?
)`)
		args = append(args, f.AuthorName)
	}
	if len(where) > 0 {
		// #nosec G202 -- every entry in `where` is a compile-time
		// string literal chosen by a static if/else on the Filter
		// struct; user-supplied values only ever land in `args` and
		// flow through the parameterized ? placeholders.
		sqlStmt += "\nWHERE " + joinWithAnd(where)
	}
	sqlStmt += "\nORDER BY b.title ASC"

	rows, err := s.db.QueryContext(ctx, sqlStmt, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list books: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BookRow
	for rows.Next() {
		br, err := scanBook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *br)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list books rows: %w", err)
	}
	// Load joined authors/categories per row. N+1 but simple; fine for
	// v0.1 library size.
	for i := range out {
		if err := s.loadJoined(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func joinWithAnd(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n    AND " + p
	}
	return out
}

// AllFilenames returns a snapshot of (filename -> stat) for every row
// in the index. Used by the sync reconciler to compute the "vanished"
// set during a full scan.
func (s *Store) AllFilenames(ctx context.Context) (map[string]FileStat, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT filename, size_bytes, mtime_ns FROM books")
	if err != nil {
		return nil, fmt.Errorf("store: read filenames: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]FileStat{}
	for rows.Next() {
		var name string
		var fs FileStat
		if err := rows.Scan(&name, &fs.SizeBytes, &fs.MtimeNanos); err != nil {
			return nil, fmt.Errorf("store: scan filename: %w", err)
		}
		out[name] = fs
	}
	return out, rows.Err()
}

// scanBook populates a *BookRow from a Row or *Rows scanner — both
// implement the single-row Scan shape we need.
func scanBook(r interface{ Scan(...any) error }) (*BookRow, error) {
	var (
		br           BookRow
		canonicalInt int
		totalPages   sql.NullInt64
		rating       sql.NullInt64
		seriesID     sql.NullInt64
		seriesIndex  sql.NullFloat64
		startedJSON  string
		finishedJSON string
		warningsJSON string
	)
	if err := r.Scan(
		&br.ID, &br.Filename, &canonicalInt, &br.Title, &br.Subtitle, &br.Publisher,
		&br.PublishDate, &totalPages, &br.ISBN, &br.Cover, &br.Format, &br.Source,
		&rating, &br.Status, &br.ReadCount, &seriesID, &seriesIndex,
		&startedJSON, &finishedJSON, &br.SizeBytes, &br.MtimeNanos,
		&br.IndexedAtUnix, &warningsJSON, &br.SeriesName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan book: %w", err)
	}
	br.CanonicalName = canonicalInt == 1
	if totalPages.Valid {
		v := totalPages.Int64
		br.TotalPages = &v
	}
	if rating.Valid {
		v := rating.Int64
		br.Rating = &v
	}
	if seriesID.Valid {
		v := seriesID.Int64
		br.SeriesID = &v
	}
	if seriesIndex.Valid {
		v := seriesIndex.Float64
		br.SeriesIndex = &v
	}
	if err := json.Unmarshal([]byte(startedJSON), &br.StartedDates); err != nil {
		return nil, fmt.Errorf("store: decode started_json: %w", err)
	}
	if err := json.Unmarshal([]byte(finishedJSON), &br.FinishedDates); err != nil {
		return nil, fmt.Errorf("store: decode finished_json: %w", err)
	}
	if err := json.Unmarshal([]byte(warningsJSON), &br.Warnings); err != nil {
		return nil, fmt.Errorf("store: decode warnings_json: %w", err)
	}
	return &br, nil
}

// loadJoined attaches authors and categories to br by querying the join
// tables.
func (s *Store) loadJoined(ctx context.Context, br *BookRow) error {
	authorRows, err := s.db.QueryContext(ctx, `
SELECT a.name FROM book_authors ba
JOIN authors a ON a.id = ba.author_id
WHERE ba.book_id = ?
ORDER BY ba.position ASC`, br.ID)
	if err != nil {
		return fmt.Errorf("store: load authors: %w", err)
	}
	br.Authors = nil
	for authorRows.Next() {
		var name string
		if err := authorRows.Scan(&name); err != nil {
			_ = authorRows.Close()
			return fmt.Errorf("store: scan author: %w", err)
		}
		br.Authors = append(br.Authors, name)
	}
	if err := authorRows.Err(); err != nil {
		_ = authorRows.Close()
		return fmt.Errorf("store: authors rows: %w", err)
	}
	_ = authorRows.Close()

	categoryRows, err := s.db.QueryContext(ctx, `
SELECT c.name FROM book_categories bc
JOIN categories c ON c.id = bc.category_id
WHERE bc.book_id = ?
ORDER BY c.name ASC`, br.ID)
	if err != nil {
		return fmt.Errorf("store: load categories: %w", err)
	}
	br.Categories = nil
	for categoryRows.Next() {
		var name string
		if err := categoryRows.Scan(&name); err != nil {
			_ = categoryRows.Close()
			return fmt.Errorf("store: scan category: %w", err)
		}
		br.Categories = append(br.Categories, name)
	}
	if err := categoryRows.Err(); err != nil {
		_ = categoryRows.Close()
		return fmt.Errorf("store: categories rows: %w", err)
	}
	_ = categoryRows.Close()
	return nil
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableFloat64(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// normalizeStringSlice replaces a nil slice with an empty one so the
// JSON encoding is "[]" rather than "null" — cleaner for downstream
// consumers and matches the column's NOT NULL DEFAULT '[]'.
func normalizeStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
