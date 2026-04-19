package audiobookshelf

import (
	"net/http"
	"net/url"
)

// Credentials carries the per-deployment bits a Client needs: the AB
// server's base URL (e.g., "http://localhost:13378") and the bearer API
// key. The zero value is not usable.
type Credentials struct {
	BaseURL string
	APIKey  string
}

// injectAuth sets Authorization: Bearer <apiKey> on req. The key is
// never written anywhere outside this header — it never lands in logs,
// error strings, or the http.Request dump form.
func (c Credentials) injectAuth(req *http.Request) {
	if c.APIKey == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
}

// redactedURL returns u with query and fragment stripped, for use in
// error messages. Path is preserved (the endpoint name helps debugging)
// but query params are removed on principle — even though Session 12
// endpoints don't send tokens in the query string, future endpoints
// might, and the convention is easier to keep uniform than remember.
func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	clone.RawQuery = ""
	clone.Fragment = ""
	return clone.String()
}
