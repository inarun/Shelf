package openlibrary

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/inarun/Shelf/internal/providers/metadata"
)

// Host allowlist — every outbound URL is constructed against one of
// these hosts in code; user input only ever lands inside the path or
// query string (and is validated/escaped before interpolation).
const (
	apiHost    = "openlibrary.org"
	coversHost = "covers.openlibrary.org"

	userAgent = "Shelf/0.1 (+https://github.com/inarun/Shelf)"

	// Response caps. JSON responses from the Open Library API are
	// typically <100 KiB — the 512 KiB cap is conservative headroom
	// without being wasteful. Cover images are capped at 2 MiB which
	// covers the "-L.jpg" large variant with margin.
	jsonMaxBytes  = 512 * 1024
	coverMaxBytes = 2 * 1024 * 1024

	requestTimeout = 15 * time.Second
	searchLimit    = 10
)

// Client is the Open Library provider. Satisfies metadata.Provider.
// Zero-value is not usable; use New to construct.
type Client struct {
	httpClient *http.Client
	baseAPI    string // overridable for tests
	baseCovers string // overridable for tests
}

// New returns a Client wired to the real Open Library endpoints. The
// underlying http.Client enforces a request timeout and a same-host
// redirect cap — cross-host redirects are rejected outright.
func New() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: checkRedirect,
		},
		baseAPI:    "https://" + apiHost,
		baseCovers: "https://" + coversHost,
	}
}

// newTestClient is the hook used by tests to point the client at a
// httptest.Server instead of the real hosts. Not exported — production
// code always uses New.
func newTestClient(baseAPI, baseCovers string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: checkRedirect,
		},
		baseAPI:    strings.TrimRight(baseAPI, "/"),
		baseCovers: strings.TrimRight(baseCovers, "/"),
	}
}

// checkRedirect caps redirect hops at 3 and rejects cross-host redirects.
// Open Library uses 302s internally (e.g., /isbn/<n>.json → /books/<OLID>.json)
// which is why we allow any hops at all.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return fmt.Errorf("openlibrary: too many redirects")
	}
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("openlibrary: cross-host redirect rejected: %s", req.URL.Host)
	}
	return nil
}

// doJSON performs a GET against the given API URL and returns the
// response body bounded to jsonMaxBytes. Callers JSON-decode the
// returned bytes. 404 maps to metadata.ErrNotFound; 2xx with a non-JSON
// Content-Type is treated as an error (prevents HTML error pages from
// being decoded as JSON).
func (c *Client) doJSON(ctx context.Context, url string) ([]byte, error) {
	// #nosec G107 -- url is built inside this package from a compile-time
	// base plus validated/escaped user input (digit-only ISBN or
	// url.QueryEscape'd search query). See caller.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("openlibrary: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openlibrary: http do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, metadata.ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openlibrary: http %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return nil, fmt.Errorf("openlibrary: unexpected content-type %q", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, jsonMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("openlibrary: read body: %w", err)
	}
	if int64(len(body)) > jsonMaxBytes {
		return nil, fmt.Errorf("openlibrary: response exceeds %d bytes", jsonMaxBytes)
	}
	return body, nil
}

// doImage fetches a cover image. Returns ErrNotFound on 404. Validates
// the Content-Type is image/jpeg or image/png and the body is within
// coverMaxBytes. Never follows cross-host redirects.
func (c *Client) doImage(ctx context.Context, url string) (*metadata.CoverImage, error) {
	// #nosec G107 -- url is built from a compile-time base plus a
	// validated cover ref (digit-only for "id:<n>" or digit/X-only for
	// "isbn:<n>"). See callers.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("openlibrary: build image request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/jpeg, image/png")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openlibrary: image do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, metadata.ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openlibrary: image http %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	ext, ok := coverExtFor(ct)
	if !ok {
		return nil, fmt.Errorf("openlibrary: unexpected cover content-type %q", ct)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, coverMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("openlibrary: read cover: %w", err)
	}
	if int64(len(data)) > coverMaxBytes {
		return nil, fmt.Errorf("openlibrary: cover exceeds %d bytes", coverMaxBytes)
	}
	if len(data) == 0 {
		return nil, metadata.ErrNotFound
	}

	return &metadata.CoverImage{
		Bytes:       data,
		ContentType: ct,
		Ext:         ext,
	}, nil
}

// coverExtFor maps an HTTP Content-Type to a file extension. Anything
// outside the allowlist returns (_, false) — callers reject the response.
func coverExtFor(contentType string) (string, bool) {
	ct := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	switch strings.ToLower(ct) {
	case "image/jpeg", "image/jpg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	default:
		return "", false
	}
}

// isValidISBN returns true if s is 10 or 13 characters long and
// consists only of digits (with a trailing 'X' allowed for ISBN-10
// check digit). The caller (LookupByISBN) uses this before
// interpolating s into a URL — the check exists for defense in depth,
// not because the URL path supports anything malicious (it's all on
// openlibrary.org and the transport just GETs), but to keep gosec happy
// and give us a clear invariant at the edge.
func isValidISBN(s string) bool {
	if len(s) != 10 && len(s) != 13 {
		return false
	}
	for i, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		if len(s) == 10 && i == 9 && (r == 'X' || r == 'x') {
			continue
		}
		return false
	}
	return true
}

