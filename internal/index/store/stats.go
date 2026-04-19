package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// StatsSummary is the aggregate view rendered on the stats page.
// StatusCounts is keyed by the frontmatter status value
// ("unread"/"reading"/"paused"/"finished"/"dnf"); missing keys mean
// zero. TotalReads sums the read_count column — a re-read book
// contributes multiple to that total. AverageRating is unrounded; the
// template formats it. RatingHistogram is keyed by rating bucket
// (1..MaxRating) — index 0 is reserved for unrated books. Bumped
// ratings (>5, via Trial-System overrides) create higher buckets on
// demand.
type StatsSummary struct {
	StatusCounts    map[string]int64
	TotalBooks      int64
	TotalReads      int64
	RatedBooks      int64
	AverageRating   float64
	RatingHistogram []int64 // RatingHistogram[n] = count of books rated n (rounded); [0] = unrated
}

// YearStats is a single-year row on the "books & pages per year"
// chart. Year is the 4-digit year string ("2024"); we keep it as a
// string because finished_json values are ISO date strings and we
// extract the year with SQL substring rather than parsing into a time.
type YearStats struct {
	Year  string
	Books int64
	Pages int64
}

// Stats returns the aggregate stats summary across all books in the
// index.
func (s *Store) Stats(ctx context.Context) (*StatsSummary, error) {
	out := &StatsSummary{StatusCounts: map[string]int64{}}

	if err := s.readStatusCounts(ctx, out); err != nil {
		return nil, err
	}
	if err := s.readAggregates(ctx, out); err != nil {
		return nil, err
	}
	if err := s.readRatingHistogram(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) readStatusCounts(ctx context.Context, sm *StatsSummary) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, count(*) FROM books GROUP BY status`)
	if err != nil {
		return fmt.Errorf("store: stats status query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return fmt.Errorf("store: stats status scan: %w", err)
		}
		sm.StatusCounts[status] = n
		sm.TotalBooks += n
	}
	return rows.Err()
}

func (s *Store) readAggregates(ctx context.Context, sm *StatsSummary) error {
	var (
		totalReads sql.NullInt64
		ratedBooks sql.NullInt64
		avgRating  sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, `
SELECT
    COALESCE(sum(read_count), 0)                                     AS total_reads,
    COALESCE(sum(CASE WHEN rating IS NOT NULL THEN 1 ELSE 0 END), 0) AS rated_books,
    COALESCE(avg(rating), 0.0)                                       AS avg_rating
FROM books
`).Scan(&totalReads, &ratedBooks, &avgRating)
	if err != nil {
		return fmt.Errorf("store: stats aggregates: %w", err)
	}
	sm.TotalReads = totalReads.Int64
	sm.RatedBooks = ratedBooks.Int64
	if ratedBooks.Int64 > 0 {
		sm.AverageRating = avgRating.Float64
	}
	return nil
}

// readRatingHistogram fills RatingHistogram from the rating column.
// Bucket 0 is reserved for unrated books; buckets 1..N are counts at
// each integer rating (SQLite stores the rounded effective value from
// the frontmatter Rating struct, see internal/vault/frontmatter.Rating).
// The slice is zero-padded up to the max observed rating so callers
// can iterate the range without sparse checks.
func (s *Store) readRatingHistogram(ctx context.Context, sm *StatsSummary) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(rating, 0) AS bucket, count(*) AS n
FROM books
GROUP BY bucket
ORDER BY bucket
`)
	if err != nil {
		return fmt.Errorf("store: rating histogram query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Accumulate into a map, then flatten to a dense slice sized to max.
	buckets := map[int64]int64{}
	var maxBucket int64
	for rows.Next() {
		var bucket, n int64
		if err := rows.Scan(&bucket, &n); err != nil {
			return fmt.Errorf("store: rating histogram scan: %w", err)
		}
		buckets[bucket] = n
		if bucket > maxBucket {
			maxBucket = bucket
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: rating histogram rows: %w", err)
	}
	// Always include at least 1..5 so the chart isn't empty when every
	// book is unrated.
	if maxBucket < 5 {
		maxBucket = 5
	}
	out := make([]int64, maxBucket+1)
	for k, v := range buckets {
		out[k] = v
	}
	sm.RatingHistogram = out
	return nil
}

// BooksPerYear groups finished reads by the year component of each
// entry in finished_json. A book re-read in two different years
// contributes to both years. Returned slice is sorted ascending by
// year.
//
// Uses SQLite's json_each virtual table (part of JSON1, bundled by
// modernc.org/sqlite). Entries with malformed date strings (anything
// whose first 4 characters aren't digits, or whose length is < 4) are
// skipped silently.
func (s *Store) BooksPerYear(ctx context.Context) ([]YearStats, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    substr(je.value, 1, 4)                                          AS year,
    count(*)                                                         AS books,
    COALESCE(sum(CASE WHEN b.total_pages IS NOT NULL
                      THEN b.total_pages
                      ELSE 0 END), 0)                                AS pages
FROM books b, json_each(b.finished_json) je
WHERE length(je.value) >= 4
  AND substr(je.value, 1, 4) GLOB '[0-9][0-9][0-9][0-9]'
GROUP BY year
ORDER BY year ASC
`)
	if err != nil {
		return nil, fmt.Errorf("store: books per year query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []YearStats
	for rows.Next() {
		var r YearStats
		if err := rows.Scan(&r.Year, &r.Books, &r.Pages); err != nil {
			return nil, fmt.Errorf("store: books per year scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: books per year rows: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out, nil
}
