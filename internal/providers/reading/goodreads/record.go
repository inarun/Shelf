package goodreads

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Record is the parsed, normalized representation of a single Goodreads
// CSV row. Field parsing quirks (Excel ISBN wrapping, slash dates,
// embedded series) are resolved at the boundary so downstream code
// (planner, apply) sees a clean typed view.
type Record struct {
	// Source row number (1-based, excluding header) for error messages.
	RowNum int

	BookID            string
	Title             string // clean title — series suffix stripped if present
	Subtitle          string
	Author            string   // primary author as it appears in CSV
	AdditionalAuthors []string // comma-split Additional Authors
	Authors           []string // [Author] + AdditionalAuthors, primary-first

	ISBN10 string // normalized, possibly "" (Excel wrappers + dashes stripped)
	ISBN13 string

	MyRating  int // 0..5; 0 means unrated
	Publisher string
	Binding   string
	TotalPages *int

	YearPublished   string
	OriginalPubYear string

	DateRead  *time.Time
	DateAdded *time.Time

	Bookshelves    []string // user shelves only (exclusive values filtered out)
	ExclusiveShelf string   // to-read | currently-reading | read

	Review    string
	ReadCount int

	OriginalTitle string

	// Derived fields, set when Title matched the "(Series, #N)" suffix.
	Series      string
	SeriesIndex *float64

	// Status is the frontmatter-level status mapped from ExclusiveShelf.
	Status string
}

var exclusiveShelves = map[string]string{
	"to-read":            "unread",
	"currently-reading":  "paused",
	"read":               "finished",
}

// seriesSuffixRe matches "(Series Name, #1)" or "(Series Name, #1.5)" at
// the end of a Goodreads title. The non-greedy capture for series name
// and the inner ban on extra parens keep the match anchored to the
// trailing group.
var seriesSuffixRe = regexp.MustCompile(`^(.*?)\s*\(([^()]+?),\s*#(\d+(?:\.\d+)?)\)\s*$`)

// isbnStripRe strips all non-alphanumeric runes. ISBN10 may end in 'X',
// uppercase — we uppercase after stripping. Both ISBN10 and ISBN13 are
// otherwise digits only.
var isbnStripRe = regexp.MustCompile(`[^0-9A-Za-z]`)

// Normalize converts a raw-row map (lowercase-keyed by header name) into
// a Record. Field-level errors accumulate in the returned slice; the
// Record is still returned with partial population so downstream code
// can decide whether the row is usable.
func Normalize(raw map[string]string, rowNum int) (Record, []error) {
	var errs []error
	r := Record{RowNum: rowNum}

	r.BookID = strings.TrimSpace(raw["book id"])
	rawTitle := strings.TrimSpace(raw["title"])
	r.Subtitle = strings.TrimSpace(raw["subtitle"])
	r.Author = strings.TrimSpace(raw["author"])
	if addl := strings.TrimSpace(raw["additional authors"]); addl != "" {
		for _, a := range strings.Split(addl, ",") {
			a = strings.TrimSpace(a)
			if a != "" {
				r.AdditionalAuthors = append(r.AdditionalAuthors, a)
			}
		}
	}
	if r.Author != "" {
		r.Authors = append([]string{r.Author}, r.AdditionalAuthors...)
	}

	r.ISBN10 = normalizeISBN(raw["isbn"])
	r.ISBN13 = normalizeISBN(raw["isbn13"])
	if r.ISBN10 != "" && !isbn10Valid(r.ISBN10) {
		r.ISBN10 = ""
	}
	if r.ISBN13 != "" && !isbn13Valid(r.ISBN13) {
		r.ISBN13 = ""
	}

	if rating := strings.TrimSpace(raw["my rating"]); rating != "" {
		if v, err := strconv.Atoi(rating); err == nil {
			if v >= 0 && v <= 5 {
				r.MyRating = v
			} else {
				errs = append(errs, fmt.Errorf("my rating %d out of range 0..5", v))
			}
		} else {
			errs = append(errs, fmt.Errorf("my rating %q not a number", rating))
		}
	}

	r.Publisher = strings.TrimSpace(raw["publisher"])
	r.Binding = strings.TrimSpace(raw["binding"])
	if pages := strings.TrimSpace(raw["number of pages"]); pages != "" {
		if v, err := strconv.Atoi(pages); err == nil && v > 0 {
			r.TotalPages = &v
		}
	}

	r.YearPublished = strings.TrimSpace(raw["year published"])
	r.OriginalPubYear = strings.TrimSpace(raw["original publication year"])
	if dr, err := parseGoodreadsDate(raw["date read"]); err != nil {
		errs = append(errs, fmt.Errorf("date read: %w", err))
	} else {
		r.DateRead = dr
	}
	if da, err := parseGoodreadsDate(raw["date added"]); err != nil {
		errs = append(errs, fmt.Errorf("date added: %w", err))
	} else {
		r.DateAdded = da
	}

	r.ExclusiveShelf = strings.TrimSpace(raw["exclusive shelf"])
	r.Status = exclusiveShelves[r.ExclusiveShelf]
	if shelves := strings.TrimSpace(raw["bookshelves"]); shelves != "" {
		for _, s := range strings.Split(shelves, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			// Exclude the three exclusive-shelf values — they're status,
			// not category tags.
			if _, isExclusive := exclusiveShelves[s]; isExclusive {
				continue
			}
			r.Bookshelves = append(r.Bookshelves, s)
		}
	}

	r.Review = raw["my review"]
	if rc := strings.TrimSpace(raw["read count"]); rc != "" {
		if v, err := strconv.Atoi(rc); err == nil && v >= 0 {
			r.ReadCount = v
		}
	}
	r.OriginalTitle = strings.TrimSpace(raw["original title"])

	// Extract series from the title if present.
	if m := seriesSuffixRe.FindStringSubmatch(rawTitle); m != nil {
		r.Title = strings.TrimSpace(m[1])
		r.Series = strings.TrimSpace(m[2])
		if idx, err := strconv.ParseFloat(m[3], 64); err == nil {
			r.SeriesIndex = &idx
		}
	} else {
		r.Title = rawTitle
	}

	return r, errs
}

// parseGoodreadsDate accepts "YYYY/MM/DD" and "YYYY-MM-DD"; empty input
// returns (nil, nil). Invalid dates return a descriptive error.
func parseGoodreadsDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	formats := []string{"2006/01/02", "2006-01-02"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("unrecognized date format %q", s)
}

// normalizeISBN strips Excel formula wrapping, dashes, and whitespace,
// then uppercases. It does NOT check length or validity — the caller
// runs the result through isbn10Valid / isbn13Valid.
func normalizeISBN(raw string) string {
	s := strings.TrimSpace(raw)
	// Excel formula: ="...". The leading = and wrapping quotes come off.
	if strings.HasPrefix(s, "=\"") && strings.HasSuffix(s, "\"") && len(s) >= 3 {
		s = s[2 : len(s)-1]
	} else if strings.HasPrefix(s, "=") {
		s = s[1:]
	}
	s = isbnStripRe.ReplaceAllString(s, "")
	return strings.ToUpper(s)
}

// isbn10Valid implements the ISO 2108 ISBN-10 checksum: sum of digit
// weights 10..1 must be divisible by 11 (X counts as 10 in the last
// position). Length must be exactly 10 after normalization.
func isbn10Valid(s string) bool {
	if len(s) != 10 {
		return false
	}
	sum := 0
	for i, r := range s {
		var d int
		switch {
		case r >= '0' && r <= '9':
			d = int(r - '0')
		case r == 'X' && i == 9:
			d = 10
		default:
			return false
		}
		sum += d * (10 - i)
	}
	return sum%11 == 0
}

// isbn13Valid implements the ISO 2108 ISBN-13 checksum: alternating 1/3
// weights sum must be divisible by 10. Length exactly 13, digits only.
func isbn13Valid(s string) bool {
	if len(s) != 13 {
		return false
	}
	sum := 0
	for i, r := range s {
		if r < '0' || r > '9' {
			return false
		}
		d := int(r - '0')
		if i%2 == 0 {
			sum += d
		} else {
			sum += d * 3
		}
	}
	return sum%10 == 0
}
