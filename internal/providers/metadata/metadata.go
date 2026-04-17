package metadata

import (
	"context"
	"errors"
)

// ErrNotFound is returned by LookupByISBN when the provider has no
// record for that ISBN. Callers treat this distinctly from a transport
// error — the add-book UI surfaces "not found" as an empty result, and
// transport errors as a failure banner.
var ErrNotFound = errors.New("metadata: not found")

// Provider is the MetadataProvider interface. Every implementation
// enforces its own host allowlist, timeouts, and response size caps.
// All three methods take a context so callers can cancel on shutdown.
type Provider interface {
	// LookupByISBN returns a single record matched on the given ISBN
	// (10 or 13, digits + X only — caller is expected to normalize).
	// Returns ErrNotFound if no record matches.
	LookupByISBN(ctx context.Context, isbn string) (*Metadata, error)

	// Search runs a free-text query (title, author, or both) and returns
	// up to a provider-defined maximum number of candidate matches. An
	// empty query returns an empty slice.
	Search(ctx context.Context, query string) ([]SearchResult, error)

	// FetchCover downloads the cover image referenced by ref. The ref is
	// an opaque string the caller got from Metadata.CoverRef or
	// SearchResult.CoverRef; its format is provider-specific. Returns
	// ErrNotFound if the provider has no cover for that ref.
	FetchCover(ctx context.Context, ref string) (*CoverImage, error)
}

// Metadata is the normalized shape returned by LookupByISBN. Pointer
// fields distinguish "not set" from zero. Field naming mirrors the
// Shelf frontmatter schema so callers can map it into a note with
// minimal translation.
type Metadata struct {
	Title       string
	Subtitle    string
	Authors     []string
	Publisher   string
	PublishDate string // "YYYY" or "YYYY-MM-DD"
	TotalPages  *int
	ISBN10      string
	ISBN13      string
	Categories  []string
	CoverRef    string // provider-specific opaque ref — feed back into FetchCover
	SourceName  string // e.g., "Open Library" — informational
	SourceID    string // stable provider key, e.g., Open Library OLID
}

// SearchResult is a single candidate from Search. Lighter than
// Metadata; callers typically LookupByISBN after the user picks one.
type SearchResult struct {
	Title       string
	Authors     []string
	PublishYear string
	ISBN        string // best ISBN the provider returned for this candidate; may be empty
	CoverRef    string
	SourceID    string
}

// CoverImage is the raw cover bytes plus enough metadata for the
// caller to store the file with a correct extension and serve it with
// a correct Content-Type.
type CoverImage struct {
	Bytes       []byte
	ContentType string // "image/jpeg" or "image/png"
	Ext         string // ".jpg" or ".png"
}
