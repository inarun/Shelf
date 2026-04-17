package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SeriesSummary is a single row on the series list page. BookCount is
// the total number of books in the series; Finished is the subset with
// status='finished'.
type SeriesSummary struct {
	ID        int64
	Name      string
	BookCount int64
	Finished  int64
}

// SeriesDetail pairs a series row with the list of its books sorted
// by series_index (NULLs last, then title).
type SeriesDetail struct {
	SeriesSummary
	Books []BookRow
}

// ListSeries returns every series that has at least one book indexed,
// ordered case-insensitively by name. Empty series (with no books) are
// excluded — the series table still holds a row but there's nothing
// to show the user.
func (s *Store) ListSeries(ctx context.Context) ([]SeriesSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    s.id,
    s.name,
    count(b.id)                                                    AS book_count,
    sum(CASE WHEN b.status = 'finished' THEN 1 ELSE 0 END)         AS finished
FROM series s
JOIN books b ON b.series_id = s.id
GROUP BY s.id, s.name
ORDER BY s.name COLLATE NOCASE ASC
`)
	if err != nil {
		return nil, fmt.Errorf("store: list series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SeriesSummary
	for rows.Next() {
		var r SeriesSummary
		var finished sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Name, &r.BookCount, &finished); err != nil {
			return nil, fmt.Errorf("store: list series scan: %w", err)
		}
		r.Finished = finished.Int64
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSeriesByName looks up a series case-insensitively and returns the
// summary plus its books ordered by series_index (NULLs last) then
// title. Returns ErrNotFound if no series row matches.
func (s *Store) GetSeriesByName(ctx context.Context, name string) (*SeriesDetail, error) {
	var summary SeriesSummary
	var finished sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
SELECT
    s.id,
    s.name,
    count(b.id)                                                    AS book_count,
    sum(CASE WHEN b.status = 'finished' THEN 1 ELSE 0 END)         AS finished
FROM series s
LEFT JOIN books b ON b.series_id = s.id
WHERE s.name = ? COLLATE NOCASE
GROUP BY s.id, s.name
`, name).Scan(&summary.ID, &summary.Name, &summary.BookCount, &finished)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get series: %w", err)
	}
	summary.Finished = finished.Int64
	if summary.BookCount == 0 {
		return nil, ErrNotFound
	}

	bookRows, err := s.db.QueryContext(ctx, `
SELECT
    b.id, b.filename, b.canonical_name, b.title, b.subtitle, b.publisher,
    b.publish_date, b.total_pages, b.isbn, b.cover, b.format, b.source,
    b.rating, b.status, b.read_count, b.series_id, b.series_index,
    b.started_json, b.finished_json, b.size_bytes, b.mtime_ns,
    b.indexed_at_unix, b.warnings_json,
    COALESCE(sr.name, '')
FROM books b
LEFT JOIN series sr ON sr.id = b.series_id
WHERE b.series_id = ?
ORDER BY (b.series_index IS NULL) ASC, b.series_index ASC, b.title ASC
`, summary.ID)
	if err != nil {
		return nil, fmt.Errorf("store: series books query: %w", err)
	}
	defer func() { _ = bookRows.Close() }()

	var books []BookRow
	for bookRows.Next() {
		br, err := scanBook(bookRows)
		if err != nil {
			return nil, err
		}
		books = append(books, *br)
	}
	if err := bookRows.Err(); err != nil {
		return nil, fmt.Errorf("store: series books rows: %w", err)
	}
	for i := range books {
		if err := s.loadJoined(ctx, &books[i]); err != nil {
			return nil, err
		}
	}

	return &SeriesDetail{SeriesSummary: summary, Books: books}, nil
}
