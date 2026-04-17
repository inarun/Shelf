package integration

import (
	"net/http/cookiejar"
)

// newCookieJar returns a fresh cookiejar for use by HTTP-level tests.
// Extracted so the import surface of http_test.go stays short.
func newCookieJar() (*cookiejar.Jar, error) {
	return cookiejar.New(nil)
}
