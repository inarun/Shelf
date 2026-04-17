package openlibrary

import (
	"context"
	"fmt"
	"strings"

	"github.com/inarun/Shelf/internal/providers/metadata"
)

// FetchCover resolves a provider-issued cover ref into raw image bytes.
// Accepted ref shapes (produced by LookupByISBN and Search):
//
//	"id:<n>"     — Open Library cover ID (digits only)
//	"olid:<n>M"  — Open Library edition ID (digits + trailing 'M')
//	"isbn:<n>"   — ISBN-10 or ISBN-13
//
// Any other shape returns an error before an HTTP request is made.
// `?default=false` asks Open Library to respond 404 instead of the
// generic placeholder when no cover exists for the ref.
func (c *Client) FetchCover(ctx context.Context, ref string) (*metadata.CoverImage, error) {
	kind, value, ok := parseCoverRef(ref)
	if !ok {
		return nil, fmt.Errorf("openlibrary: invalid cover ref %q", ref)
	}
	url := fmt.Sprintf("%s/b/%s/%s-L.jpg?default=false", c.baseCovers, kind, value)
	return c.doImage(ctx, url)
}

// parseCoverRef splits "<kind>:<value>" and validates value against
// the kind's expected alphabet. Returns (kind, value, ok).
func parseCoverRef(ref string) (kind, value string, ok bool) {
	idx := strings.IndexByte(ref, ':')
	if idx <= 0 || idx == len(ref)-1 {
		return "", "", false
	}
	kind = strings.ToLower(ref[:idx])
	value = ref[idx+1:]
	switch kind {
	case "id":
		if !isAllDigits(value) {
			return "", "", false
		}
		// cap length to keep URL sane
		if len(value) > 12 {
			return "", "", false
		}
		return "id", value, true
	case "olid":
		// OLID edition IDs look like OL<digits>M. Case-insensitive on
		// the prefix, strict on the body.
		up := strings.ToUpper(value)
		if !strings.HasPrefix(up, "OL") || !strings.HasSuffix(up, "M") {
			return "", "", false
		}
		mid := up[2 : len(up)-1]
		if len(mid) == 0 || len(mid) > 10 || !isAllDigits(mid) {
			return "", "", false
		}
		return "olid", up, true
	case "isbn":
		up := strings.ToUpper(value)
		if !isValidISBN(up) {
			return "", "", false
		}
		return "isbn", up, true
	default:
		return "", "", false
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

