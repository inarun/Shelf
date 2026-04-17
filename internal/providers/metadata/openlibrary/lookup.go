package openlibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/inarun/Shelf/internal/providers/metadata"
)

// LookupByISBN hits /api/books?bibkeys=ISBN:<isbn>&format=json&jscmd=data.
// The "data" jscmd flavor resolves author names inline, so we don't
// need to fetch each /authors/<OLID> record separately.
//
// Returns metadata.ErrNotFound if the response is present but contains
// no entry for the requested ISBN (Open Library returns {} in that
// case, not 404).
func (c *Client) LookupByISBN(ctx context.Context, isbn string) (*metadata.Metadata, error) {
	isbn = strings.ToUpper(strings.TrimSpace(isbn))
	if !isValidISBN(isbn) {
		return nil, fmt.Errorf("openlibrary: invalid isbn %q", isbn)
	}
	url := fmt.Sprintf("%s/api/books?bibkeys=ISBN:%s&format=json&jscmd=data", c.baseAPI, isbn)
	body, err := c.doJSON(ctx, url)
	if err != nil {
		return nil, err
	}

	// The response is a map keyed by "ISBN:<isbn>". Missing ISBNs get an
	// empty object — treat that as not-found.
	var parsed map[string]olBookData
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("openlibrary: decode lookup: %w", err)
	}
	key := "ISBN:" + isbn
	rec, ok := parsed[key]
	if !ok || rec.isEmpty() {
		return nil, metadata.ErrNotFound
	}
	return rec.toMetadata(isbn), nil
}

// olBookData matches the jscmd=data envelope from /api/books. Open
// Library ships every field as optional; pointer/slice zero-values mean
// the field is absent from the response.
type olBookData struct {
	Title         string             `json:"title"`
	Subtitle      string             `json:"subtitle"`
	Authors       []olNamedRef       `json:"authors"`
	Publishers    []olNamedRef       `json:"publishers"`
	PublishDate   string             `json:"publish_date"`
	NumberOfPages *int               `json:"number_of_pages"`
	Identifiers   olIdentifiers      `json:"identifiers"`
	Subjects      []olNamedRef       `json:"subjects"`
	Cover         *olCover           `json:"cover"`
	URL           string             `json:"url"`
	Key           string             `json:"key"`
	WorkKeys      []olKeyRef         `json:"works"`
	Classifications map[string]any   `json:"classifications"`
}

type olNamedRef struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Key  string `json:"key"`
}

type olKeyRef struct {
	Key string `json:"key"`
}

type olIdentifiers struct {
	ISBN10 []string `json:"isbn_10"`
	ISBN13 []string `json:"isbn_13"`
	OpenLibrary []string `json:"openlibrary"`
	Goodreads []string `json:"goodreads"`
}

type olCover struct {
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Large  string `json:"large"`
}

// isEmpty reports whether the record has literally no fields populated
// — Open Library sometimes includes a stub with just a URL/key for an
// ISBN it doesn't really know about.
func (r olBookData) isEmpty() bool {
	return r.Title == "" && len(r.Authors) == 0 && len(r.Identifiers.ISBN10) == 0 &&
		len(r.Identifiers.ISBN13) == 0 && r.Cover == nil
}

// toMetadata maps an olBookData into the normalized metadata.Metadata
// the rest of Shelf consumes. The fallback ISBN10/13 values are chosen
// so the caller's downstream "which ISBN do I put in the frontmatter"
// rule (ISBN-10 preferred, else ISBN-13) has both available.
func (r olBookData) toMetadata(queriedISBN string) *metadata.Metadata {
	m := &metadata.Metadata{
		Title:       strings.TrimSpace(r.Title),
		Subtitle:    strings.TrimSpace(r.Subtitle),
		PublishDate: strings.TrimSpace(r.PublishDate),
		TotalPages:  r.NumberOfPages,
		Authors:     namedRefNames(r.Authors),
		SourceName:  "Open Library",
	}
	if len(r.Publishers) > 0 {
		m.Publisher = strings.TrimSpace(r.Publishers[0].Name)
	}
	if len(r.Identifiers.ISBN10) > 0 {
		m.ISBN10 = strings.TrimSpace(r.Identifiers.ISBN10[0])
	}
	if len(r.Identifiers.ISBN13) > 0 {
		m.ISBN13 = strings.TrimSpace(r.Identifiers.ISBN13[0])
	}
	// Fall back to the queried ISBN if identifiers weren't returned —
	// rare but happens on older editions.
	if m.ISBN10 == "" && len(queriedISBN) == 10 {
		m.ISBN10 = queriedISBN
	}
	if m.ISBN13 == "" && len(queriedISBN) == 13 {
		m.ISBN13 = queriedISBN
	}

	// OLID from r.Key ("/books/OL123M") — safe to leave empty on failure.
	if strings.HasPrefix(r.Key, "/books/") {
		m.SourceID = strings.TrimPrefix(r.Key, "/books/")
	}

	// Pick a cover reference that FetchCover can resolve. We prefer an
	// OLID-based ref because it's stable across editions, then fall back
	// to the ISBN we just queried.
	switch {
	case m.SourceID != "":
		m.CoverRef = "olid:" + m.SourceID
	case m.ISBN13 != "":
		m.CoverRef = "isbn:" + m.ISBN13
	case m.ISBN10 != "":
		m.CoverRef = "isbn:" + m.ISBN10
	}

	// Subjects are Open Library's closest analog to categories. Keep at
	// most 5 and drop the deeply-generic "Fiction" / "Nonfiction" tags.
	m.Categories = pickCategories(r.Subjects)
	return m
}

// namedRefNames flattens []olNamedRef to []string, skipping empty names.
func namedRefNames(refs []olNamedRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		n := strings.TrimSpace(ref.Name)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// pickCategories returns up to 5 subject names, filtered against a
// handful of overly generic tags.
func pickCategories(subjects []olNamedRef) []string {
	skip := map[string]bool{
		"fiction":    true,
		"nonfiction": true,
		"non-fiction": true,
	}
	out := make([]string, 0, 5)
	for _, s := range subjects {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		if skip[strings.ToLower(name)] {
			continue
		}
		out = append(out, name)
		if len(out) == 5 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
