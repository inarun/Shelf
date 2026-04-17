package goodreads

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaxTotalBytes caps the total CSV input at 16 MiB. Nusayb's
// ~123-book export is well under 1 MiB; anything above this is either
// corrupted or hostile.
const DefaultMaxTotalBytes int64 = 16 << 20

// DefaultMaxFieldBytes caps any single field at 1 MiB. Reviews can be
// long but never this long in practice; pathological inputs would
// otherwise force unbounded allocation.
const DefaultMaxFieldBytes = 1 << 20

// Reader parses a Goodreads CSV export. The zero value is not usable —
// call NewReader.
type Reader struct {
	r              io.Reader
	maxFieldBytes  int
	maxTotalBytes  int64
}

// NewReader wraps r for Goodreads CSV parsing. Defaults enforce the
// size caps documented on Default* constants; callers may override via
// SetMaxFieldBytes / SetMaxTotalBytes.
func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:             r,
		maxFieldBytes: DefaultMaxFieldBytes,
		maxTotalBytes: DefaultMaxTotalBytes,
	}
}

// SetMaxFieldBytes overrides the per-field byte cap. Zero or negative
// values disable the cap.
func (r *Reader) SetMaxFieldBytes(n int) { r.maxFieldBytes = n }

// SetMaxTotalBytes overrides the total-input byte cap. Zero or negative
// values disable the cap.
func (r *Reader) SetMaxTotalBytes(n int64) { r.maxTotalBytes = n }

// ReadAll consumes every row, emitting a []Record and an aggregated
// error for rows that couldn't be parsed. The header row builds a
// case-insensitive column-name → index map so column reorders across
// Goodreads export revisions are tolerated.
//
// Rows that individually fail (malformed quoting, oversized fields)
// are skipped and their errors accumulate in the returned error (nil
// when every row parsed). Successful rows still return in the slice.
func (r *Reader) ReadAll() ([]Record, error) {
	input := r.r
	if r.maxTotalBytes > 0 {
		// +1 so we can detect exact-size overflows rather than silently
		// truncating at the boundary.
		input = io.LimitReader(input, r.maxTotalBytes+1)
	}

	cr := csv.NewReader(input)
	cr.LazyQuotes = false
	cr.FieldsPerRecord = -1 // tolerate trailing-comma rows across exports

	header, err := cr.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("goodreads: empty CSV")
		}
		return nil, fmt.Errorf("goodreads: read header: %w", err)
	}
	// Strip a UTF-8 BOM from the first header cell if present.
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	headerIdx := make(map[string]int, len(header))
	for i, name := range header {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if _, dup := headerIdx[key]; dup {
			// Duplicate header name — keep the first. Goodreads hasn't
			// shipped duplicates in practice; surface a diagnostic rather
			// than silently overwriting.
			continue
		}
		headerIdx[key] = i
	}

	var (
		out      []Record
		errs     []error
		rowNum   int
		totalIn  int64
	)
	for {
		rowNum++
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("row %d: %w", rowNum, err))
			continue
		}
		if r.maxFieldBytes > 0 {
			oversized := false
			for _, field := range row {
				if len(field) > r.maxFieldBytes {
					errs = append(errs, fmt.Errorf("row %d: field exceeds %d bytes", rowNum, r.maxFieldBytes))
					oversized = true
					break
				}
				totalIn += int64(len(field))
			}
			if oversized {
				continue
			}
		}

		raw := make(map[string]string, len(headerIdx))
		for name, idx := range headerIdx {
			if idx < len(row) {
				raw[name] = row[idx]
			}
		}
		rec, recErrs := Normalize(raw, rowNum)
		out = append(out, rec)
		if len(recErrs) > 0 {
			for _, e := range recErrs {
				errs = append(errs, fmt.Errorf("row %d: %w", rowNum, e))
			}
		}
	}

	if r.maxTotalBytes > 0 && totalIn > r.maxTotalBytes {
		errs = append(errs, fmt.Errorf("goodreads: input exceeded total cap %d bytes", r.maxTotalBytes))
	}
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}
