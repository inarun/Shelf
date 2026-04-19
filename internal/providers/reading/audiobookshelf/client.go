package audiobookshelf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	userAgent = "Shelf/0.1 (+https://github.com/inarun/Shelf)"

	// Response cap. Audiobookshelf's /api/me/listening-sessions can
	// return moderately large paginated JSON; 5 MiB per page is the
	// SKILL.md §v0.2 Session 12 budget.
	jsonMaxBytes = 5 * 1024 * 1024

	requestTimeout = 15 * time.Second

	maxRedirects = 3
)

// ErrNotConfigured indicates the caller constructed a Client-dependent
// code path while the provider is disabled. The runtime wiring in
// cmd/shelf keeps a nil *Client when config says disabled; handlers
// detect nil and return 503 rather than calling methods.
var ErrNotConfigured = errors.New("audiobookshelf: client not configured")

// Client is a read-only Audiobookshelf API client. Zero value is not
// usable; construct via New.
type Client struct {
	httpClient *http.Client
	base       *url.URL
	creds      Credentials
}

// New constructs a Client pointed at the AB server described by creds.
// baseURL must be http or https; AB is commonly deployed on LAN over
// plain HTTP so both schemes are accepted. Empty base URL or API key
// is rejected early.
func New(creds Credentials) (*Client, error) {
	if strings.TrimSpace(creds.BaseURL) == "" {
		return nil, errors.New("audiobookshelf: base_url required")
	}
	if strings.TrimSpace(creds.APIKey) == "" {
		return nil, errors.New("audiobookshelf: api_key required")
	}
	u, err := url.Parse(strings.TrimRight(creds.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("audiobookshelf: parse base_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("audiobookshelf: base_url scheme must be http or https; got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("audiobookshelf: base_url must have a host")
	}
	return &Client{
		httpClient: &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: makeCheckRedirect(u.Host),
		},
		base:  u,
		creds: creds,
	}, nil
}

// makeCheckRedirect returns a CheckRedirect that caps redirect hops at
// maxRedirects and rejects any redirect whose host doesn't match the
// original configured AB host. This prevents a compromised or
// misconfigured AB instance from redirecting credentials elsewhere.
func makeCheckRedirect(allowedHost string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("audiobookshelf: too many redirects")
		}
		if req.URL.Host != allowedHost {
			return fmt.Errorf("audiobookshelf: cross-host redirect rejected")
		}
		return nil
	}
}

// GetMe returns the authenticated user (/api/me).
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	if err := c.doJSON(ctx, "/api/me", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetItemsInProgress returns the authenticated user's in-progress
// library items (/api/me/items-in-progress).
func (c *Client) GetItemsInProgress(ctx context.Context) (*ItemsInProgressResponse, error) {
	var resp ItemsInProgressResponse
	if err := c.doJSON(ctx, "/api/me/items-in-progress", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetListeningSessions returns a page of listening sessions for the
// authenticated user (/api/me/listening-sessions). page is zero-indexed
// per AB's convention; itemsPerPage is capped server-side.
func (c *Client) GetListeningSessions(ctx context.Context, page, itemsPerPage int) (*ListeningSessionsResponse, error) {
	q := url.Values{}
	if itemsPerPage > 0 {
		q.Set("itemsPerPage", strconv.Itoa(itemsPerPage))
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var resp ListeningSessionsResponse
	if err := c.doJSON(ctx, "/api/me/listening-sessions", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// doJSON performs a GET against path (relative to the configured base
// URL), injects the bearer token, enforces the JSON content-type
// allowlist + size cap, and decodes the response body into out.
func (c *Client) doJSON(ctx context.Context, path string, query url.Values, out any) error {
	reqURL := *c.base
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + path
	if query != nil {
		reqURL.RawQuery = query.Encode()
	}

	// #nosec G107 -- reqURL is built from the configured base URL plus a
	// compile-time path constant. No user-supplied URL fragments reach
	// this call site.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("audiobookshelf: build request for %s: %w", path, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	c.creds.injectAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// err may contain the URL (net/url.Error does), but because we
		// use a clean base URL + path with no query secrets the URL is
		// safe to surface. The API key is in a request header, not the
		// URL, so it never appears in any http.Client error.
		return fmt.Errorf("audiobookshelf: http do %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("audiobookshelf: http %d from %s", resp.StatusCode, redactedURL(&reqURL))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("audiobookshelf: unexpected content-type %q from %s", ct, redactedURL(&reqURL))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, jsonMaxBytes+1))
	if err != nil {
		return fmt.Errorf("audiobookshelf: read body for %s: %w", path, err)
	}
	if int64(len(body)) > jsonMaxBytes {
		return fmt.Errorf("audiobookshelf: response from %s exceeds %d bytes", redactedURL(&reqURL), jsonMaxBytes)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("audiobookshelf: decode %s: %w", path, err)
	}
	return nil
}
