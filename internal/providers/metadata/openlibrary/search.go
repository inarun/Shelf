package openlibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/inarun/Shelf/internal/providers/metadata"
)

// Search hits /search.json with a minimal `fields` projection. The
// query is trimmed and URL-escaped before interpolation; an empty
// query returns an empty slice with no HTTP call made.
func (c *Client) Search(ctx context.Context, query string) ([]metadata.SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	escaped := url.QueryEscape(q)
	fields := "key,title,author_name,first_publish_year,isbn,cover_i"
	u := fmt.Sprintf("%s/search.json?q=%s&limit=%d&fields=%s",
		c.baseAPI, escaped, searchLimit, fields)

	body, err := c.doJSON(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp olSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openlibrary: decode search: %w", err)
	}

	out := make([]metadata.SearchResult, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		r := metadata.SearchResult{
			Title:   strings.TrimSpace(d.Title),
			Authors: trimAll(d.Authors),
		}
		if d.FirstPublishYear > 0 {
			r.PublishYear = strconv.Itoa(d.FirstPublishYear)
		}
		// Pick the first syntactically-valid ISBN from the returned list;
		// Open Library's ISBN array mixes editions and languages without
		// guarantees of normalization.
		for _, candidate := range d.ISBN {
			candidate = strings.ToUpper(strings.TrimSpace(candidate))
			if isValidISBN(candidate) {
				r.ISBN = candidate
				break
			}
		}
		// Prefer cover_i (stable cover ID) over deriving from ISBN — the
		// cover ID is the exact match for the edition the search picked.
		if d.CoverID != 0 {
			r.CoverRef = "id:" + strconv.Itoa(d.CoverID)
		} else if r.ISBN != "" {
			r.CoverRef = "isbn:" + r.ISBN
		}
		if strings.HasPrefix(d.Key, "/works/") {
			r.SourceID = strings.TrimPrefix(d.Key, "/works/")
		}
		out = append(out, r)
	}
	return out, nil
}

// olSearchResponse is the shape of /search.json with the field
// projection we request.
type olSearchResponse struct {
	Docs []olSearchDoc `json:"docs"`
}

type olSearchDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	Authors          []string `json:"author_name"`
	FirstPublishYear int      `json:"first_publish_year"`
	ISBN             []string `json:"isbn"`
	CoverID          int      `json:"cover_i"`
}

// trimAll trims whitespace from every string in s and drops empties.
func trimAll(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for _, v := range s {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
