package llm

import (
	"net/http"
	"net/url"
)

// Credentials carries the per-deployment bits a Client needs: the
// Anthropic API base URL and the user's API key. The zero value is
// not usable.
type Credentials struct {
	BaseURL string
	APIKey  string
}

// injectAuth sets the two headers Anthropic requires: x-api-key
// carrying the user's key verbatim (no Bearer prefix), and
// anthropic-version pinning the API surface this client speaks. The
// key is never written anywhere outside this header — it never lands
// in logs, error strings, or the http.Request dump form.
func (c Credentials) injectAuth(req *http.Request) {
	if c.APIKey == "" {
		return
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

// redactedURL returns u with query and fragment stripped, for use in
// error messages. Path is preserved (the endpoint name helps
// debugging) but query params are removed on principle — the
// Anthropic API doesn't put secrets in query strings today, but the
// convention is easier to keep uniform than remember.
func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}
