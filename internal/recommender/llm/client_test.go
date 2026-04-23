package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testAPIKey = "test-key-do-not-log-0123456789"
	testModel  = "claude-haiku-4-5"
)

func newServerClient(t *testing.T, mux http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(Config{BaseURL: srv.URL, APIKey: testAPIKey, Model: testModel})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func assertKeyNotInError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("API key leaked in error: %v", err)
	}
}

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://host", Model: testModel}); err == nil {
		t.Error("New: want error for empty APIKey")
	}
}

func TestNew_RequiresModel(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://host", APIKey: "k"}); err == nil {
		t.Error("New: want error for empty Model")
	}
}

func TestNew_DefaultsBaseURLToAnthropic(t *testing.T) {
	c, err := New(Config{APIKey: "k", Model: testModel})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.base.Host != "api.anthropic.com" {
		t.Errorf("base.Host=%q, want api.anthropic.com", c.base.Host)
	}
	if c.base.Scheme != "https" {
		t.Errorf("base.Scheme=%q, want https", c.base.Scheme)
	}
}

func TestNew_RejectsInvalidScheme(t *testing.T) {
	if _, err := New(Config{BaseURL: "ftp://host", APIKey: "k", Model: testModel}); err == nil {
		t.Error("New: want error for ftp scheme")
	}
}

func TestNew_AcceptsHTTP(t *testing.T) {
	if _, err := New(Config{BaseURL: "http://localhost:1234", APIKey: "k", Model: testModel}); err != nil {
		t.Errorf("New(http): %v", err)
	}
}

func TestNew_AcceptsHTTPS(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://api.anthropic.com", APIKey: "k", Model: testModel}); err != nil {
		t.Errorf("New(https): %v", err)
	}
}

func TestModel_ReturnsConfigured(t *testing.T) {
	c, _ := New(Config{APIKey: "k", Model: testModel})
	if got := c.Model(); got != testModel {
		t.Errorf("Model()=%q, want %q", got, testModel)
	}
}

func TestPing_ParsesModelsFixture(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != testAPIKey {
			t.Errorf("x-api-key = %q, want %s", got, testAPIKey)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Errorf("anthropic-version = %q, want %s", got, anthropicVersion)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty (Anthropic uses x-api-key not Bearer)", got)
		}
		serveFixture(t, w, "models.json")
	})
	c, _ := newServerClient(t, mux)

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestPing_EnforcesContentType(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>oops</html>"))
	})
	c, _ := newServerClient(t, mux)

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("want error for text/html response")
	}
	if !strings.Contains(err.Error(), "content-type") {
		t.Errorf("err=%v, want content-type error", err)
	}
	assertKeyNotInError(t, err)
}

func TestPing_EnforcesSizeCap(t *testing.T) {
	// 513 KiB payload — above the 512 KiB cap.
	payload := bytes.Repeat([]byte("a"), jsonMaxBytes+1024)
	wrapped := append(append([]byte(`{"data":[{"id":"`), payload...), []byte(`"}]}`)...)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wrapped)
	})
	c, _ := newServerClient(t, mux)

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("want error for oversized response")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err=%v, want size-cap error", err)
	}
	assertKeyNotInError(t, err)
}

func TestPing_RejectsNon2xx(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c, _ := newServerClient(t, mux)

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("want error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err=%v, want 401 mentioned", err)
	}
	assertKeyNotInError(t, err)
}

func TestPing_RejectsCrossHostRedirect(t *testing.T) {
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(serverB.Close)

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, serverB.URL+"/v1/models", http.StatusFound)
	}))
	t.Cleanup(serverA.Close)

	c, err := New(Config{BaseURL: serverA.URL, APIKey: testAPIKey, Model: testModel})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Ping(context.Background())
	if err == nil {
		t.Fatal("want error for cross-host redirect")
	}
	if !strings.Contains(err.Error(), "cross-host") && !strings.Contains(err.Error(), "redirect") {
		t.Errorf("err=%v, want redirect-rejection", err)
	}
	assertKeyNotInError(t, err)
}

func TestPing_RejectsRedirectChain(t *testing.T) {
	var hops atomic.Int32
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		http.Redirect(w, r, srv.URL+"/v1/models", http.StatusFound)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New(Config{BaseURL: srv.URL, APIKey: testAPIKey, Model: testModel})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Ping(context.Background())
	if err == nil {
		t.Fatal("want error for redirect chain")
	}
	if !strings.Contains(err.Error(), "too many redirects") {
		t.Errorf("err=%v, want too-many-redirects error", err)
	}
	if hops.Load() > int32(maxRedirects)+1 {
		t.Errorf("hops=%d, want ≤ %d+1", hops.Load(), maxRedirects)
	}
	assertKeyNotInError(t, err)
}

func TestPing_HonorsContextTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	})
	c, _ := newServerClient(t, mux)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.Ping(ctx)
	if err == nil {
		t.Fatal("want error for context deadline")
	}
	assertKeyNotInError(t, err)
}

func TestInjectAuth_SetsXAPIKeyHeader(t *testing.T) {
	creds := Credentials{BaseURL: "http://x", APIKey: "xyz"}
	req, _ := http.NewRequest(http.MethodGet, "http://x/a", nil)
	creds.injectAuth(req)
	if got := req.Header.Get("x-api-key"); got != "xyz" {
		t.Errorf("x-api-key=%q, want xyz", got)
	}
	// Regression guard: Anthropic does NOT use Authorization: Bearer —
	// a blind copy-paste from audiobookshelf would set that header and
	// the API would reject with 401 at runtime.
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization=%q, want empty (use x-api-key for Anthropic)", got)
	}
}

func TestInjectAuth_SetsAnthropicVersionHeader(t *testing.T) {
	creds := Credentials{BaseURL: "http://x", APIKey: "xyz"}
	req, _ := http.NewRequest(http.MethodGet, "http://x/a", nil)
	creds.injectAuth(req)
	if got := req.Header.Get("anthropic-version"); got != anthropicVersion {
		t.Errorf("anthropic-version=%q, want %s", got, anthropicVersion)
	}
}

func TestInjectAuth_NoHeaderForEmptyKey(t *testing.T) {
	creds := Credentials{APIKey: ""}
	req, _ := http.NewRequest(http.MethodGet, "http://x/a", nil)
	creds.injectAuth(req)
	if got := req.Header.Get("x-api-key"); got != "" {
		t.Errorf("want no header for empty key; got %q", got)
	}
	if got := req.Header.Get("anthropic-version"); got != "" {
		t.Errorf("anthropic-version=%q, want empty when key absent", got)
	}
}

func TestRedactedURL_StripsQueryAndFragment(t *testing.T) {
	u, _ := url.Parse("https://api.anthropic.com/v1/models?token=secret&p=1#frag")
	got := redactedURL(u)
	if strings.Contains(got, "secret") {
		t.Errorf("redactedURL leaked query: %s", got)
	}
	if strings.Contains(got, "frag") {
		t.Errorf("redactedURL leaked fragment: %s", got)
	}
	if !strings.Contains(got, "/v1/models") {
		t.Errorf("redactedURL lost path: %s", got)
	}
}

func TestRedactedURL_NilIsEmptyString(t *testing.T) {
	if got := redactedURL(nil); got != "" {
		t.Errorf("redactedURL(nil)=%q, want empty", got)
	}
}

// serveFixture reads a JSON fixture from testdata/ and writes it to w.
func serveFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var scratch any
	if err := json.Unmarshal(data, &scratch); err != nil {
		t.Fatalf("fixture %s is not valid JSON: %v", name, err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
