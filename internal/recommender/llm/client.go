package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotConfigured indicates a caller reached a client-dependent code
// path while the provider is disabled. The runtime wiring in cmd/shelf
// keeps a nil *Client when config says disabled; handlers (S23) will
// detect nil and return 503 rather than calling methods.
var ErrNotConfigured = errors.New("llm: client not configured")

// Client is a read-only Anthropic API client. Zero value is not
// usable; construct via New.
type Client struct {
	httpClient *http.Client
	base       *url.URL
	creds      Credentials
	model      string
}

// New constructs a Client from cfg. BaseURL defaults to the public
// Anthropic API when empty. APIKey and Model are required. Only http
// and https schemes are accepted; tests use http against an
// httptest.Server, production uses https against api.anthropic.com.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("llm: api_key required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm: model required")
	}

	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = anthropicAPIBase
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return nil, fmt.Errorf("llm: parse base_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("llm: base_url scheme must be http or https; got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("llm: base_url must have a host")
	}

	return &Client{
		httpClient: &http.Client{
			Timeout:       requestTimeout,
			CheckRedirect: makeCheckRedirect(u.Host),
		},
		base:  u,
		creds: Credentials{BaseURL: base, APIKey: cfg.APIKey},
		model: cfg.Model,
	}, nil
}

// Model returns the configured model ID. Exposed so callers (S23) can
// record which model produced a TunedWeights artifact without
// re-threading config through the Tune path.
func (c *Client) Model() string { return c.model }

// makeCheckRedirect returns a CheckRedirect that caps redirect hops
// at maxRedirects and rejects any redirect whose host doesn't match
// the original configured host. This prevents a compromised or
// misconfigured upstream from redirecting credentials elsewhere.
func makeCheckRedirect(allowedHost string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("llm: too many redirects")
		}
		if req.URL.Host != allowedHost {
			return fmt.Errorf("llm: cross-host redirect rejected")
		}
		return nil
	}
}

// ModelsResponse is the minimal shape of GET /v1/models — just enough
// to confirm the response parses. The full schema is larger; this
// client only reads what Ping needs.
type ModelsResponse struct {
	Data []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Type        string `json:"type"`
	} `json:"data"`
}

// Ping verifies connectivity and auth by calling GET /v1/models.
// Returns nil on a 2xx JSON response that parses; any transport,
// status, content-type, size-cap, or parse error bubbles up. Zero
// token cost on Anthropic's side — this is the intended health check.
func (c *Client) Ping(ctx context.Context) error {
	var resp ModelsResponse
	return c.doJSON(ctx, http.MethodGet, "/v1/models", &resp)
}

// doJSON performs a bodyless request against path (relative to the
// configured base URL), injects the Anthropic auth headers, enforces
// the JSON content-type allowlist + size cap, and decodes the
// response body into out. Kept private so callers see a stable
// per-endpoint surface.
func (c *Client) doJSON(ctx context.Context, method, path string, out any) error {
	reqURL := *c.base
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + path

	// #nosec G107 -- reqURL is built from the configured base URL plus a
	// compile-time path constant. No user-supplied URL fragments reach
	// this call site.
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
	if err != nil {
		return fmt.Errorf("llm: build request for %s: %w", path, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	c.creds.injectAuth(req)

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		// err may contain the URL (net/url.Error does), but because we
		// use a clean base URL + path with no query secrets the URL is
		// safe to surface. The API key lives in headers, not the URL,
		// so it never appears in any http.Client error.
		return fmt.Errorf("llm: http do %s: %w", path, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("llm: http %d from %s", httpResp.StatusCode, redactedURL(&reqURL))
	}

	ct := httpResp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("llm: unexpected content-type %q from %s", ct, redactedURL(&reqURL))
	}

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, jsonMaxBytes+1))
	if err != nil {
		return fmt.Errorf("llm: read body for %s: %w", path, err)
	}
	if int64(len(body)) > jsonMaxBytes {
		return fmt.Errorf("llm: response from %s exceeds %d bytes", redactedURL(&reqURL), jsonMaxBytes)
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("llm: decode %s: %w", path, err)
	}
	return nil
}
