package audiobookshelf

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

const testAPIKey = "test-key-do-not-log-0123456789"

func newServerClient(t *testing.T, mux http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(Credentials{BaseURL: srv.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestNew_RequiresBaseURL(t *testing.T) {
	if _, err := New(Credentials{APIKey: "k"}); err == nil {
		t.Error("New: want error for empty BaseURL")
	}
}

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Credentials{BaseURL: "http://host"}); err == nil {
		t.Error("New: want error for empty APIKey")
	}
}

func TestNew_RejectsInvalidScheme(t *testing.T) {
	if _, err := New(Credentials{BaseURL: "ftp://host", APIKey: "k"}); err == nil {
		t.Error("New: want error for ftp scheme")
	}
}

func TestNew_AcceptsHTTP(t *testing.T) {
	// AB is typically self-hosted on LAN; http is fine.
	if _, err := New(Credentials{BaseURL: "http://localhost:13378", APIKey: "k"}); err != nil {
		t.Errorf("New(http): %v", err)
	}
}

func TestNew_AcceptsHTTPS(t *testing.T) {
	if _, err := New(Credentials{BaseURL: "https://ab.example", APIKey: "k"}); err != nil {
		t.Errorf("New(https): %v", err)
	}
}

func TestGetMe_ParsesFixture(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testAPIKey {
			t.Errorf("Authorization = %q, want Bearer …", got)
		}
		serveFixture(t, w, "me.json")
	})
	c, _ := newServerClient(t, mux)

	u, err := c.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if u.Username != "test-user" {
		t.Errorf("Username=%q", u.Username)
	}
	if u.Type != "user" {
		t.Errorf("Type=%q", u.Type)
	}
	if u.ID == "" {
		t.Error("ID empty")
	}
}

func TestGetItemsInProgress_ParsesFixture(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me/items-in-progress", func(w http.ResponseWriter, _ *http.Request) {
		serveFixture(t, w, "items_in_progress.json")
	})
	c, _ := newServerClient(t, mux)

	resp, err := c.GetItemsInProgress(context.Background())
	if err != nil {
		t.Fatalf("GetItemsInProgress: %v", err)
	}
	if len(resp.LibraryItems) != 2 {
		t.Fatalf("libraryItems=%d, want 2", len(resp.LibraryItems))
	}
	hy := resp.LibraryItems[0]
	if hy.ID != "li_hyperion_01" {
		t.Errorf("id=%q", hy.ID)
	}
	if hy.Media.Metadata.Title != "Hyperion" {
		t.Errorf("title=%q", hy.Media.Metadata.Title)
	}
	if hy.Media.Metadata.ASIN != "B003GAN4HE" {
		t.Errorf("asin=%q", hy.Media.Metadata.ASIN)
	}
	if hy.IsFinished {
		t.Error("isFinished: want false")
	}
	phm := resp.LibraryItems[1]
	if !phm.IsFinished {
		t.Error("phm isFinished: want true")
	}
	if phm.Media.Metadata.ISBN != "9780593135204" {
		t.Errorf("phm isbn=%q", phm.Media.Metadata.ISBN)
	}
}

func TestGetListeningSessions_ParsesFixture(t *testing.T) {
	var gotQuery url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me/listening-sessions", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		serveFixture(t, w, "listening_sessions.json")
	})
	c, _ := newServerClient(t, mux)

	resp, err := c.GetListeningSessions(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetListeningSessions: %v", err)
	}
	if gotQuery.Get("itemsPerPage") != "10" {
		t.Errorf("itemsPerPage query=%q", gotQuery.Get("itemsPerPage"))
	}
	if resp.Total != 2 {
		t.Errorf("total=%d", resp.Total)
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("sessions=%d", len(resp.Sessions))
	}
	if resp.Sessions[0].LibraryItemID != "li_hyperion_01" {
		t.Errorf("sessions[0].libraryItemId=%q", resp.Sessions[0].LibraryItemID)
	}
	if resp.Sessions[0].TimeListening != 3600.25 {
		t.Errorf("timeListening=%v", resp.Sessions[0].TimeListening)
	}
}

func TestDoJSON_EnforcesContentType(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>oops</html>"))
	})
	c, _ := newServerClient(t, mux)

	_, err := c.GetMe(context.Background())
	if err == nil {
		t.Fatal("want error for text/html response")
	}
	if !strings.Contains(err.Error(), "content-type") {
		t.Errorf("err=%v, want content-type error", err)
	}
	assertKeyNotInError(t, err)
}

func TestDoJSON_EnforcesSizeCap(t *testing.T) {
	// Generate a 6 MiB JSON payload — above the 5 MiB cap.
	payload := bytes.Repeat([]byte("a"), jsonMaxBytes+1024)
	wrapped := append(append([]byte(`{"x":"`), payload...), []byte(`"}`)...)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wrapped)
	})
	c, _ := newServerClient(t, mux)

	_, err := c.GetMe(context.Background())
	if err == nil {
		t.Fatal("want error for oversized response")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err=%v, want size-cap error", err)
	}
	assertKeyNotInError(t, err)
}

func TestDoJSON_RejectsNon2xx(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, _ := newServerClient(t, mux)

	_, err := c.GetMe(context.Background())
	if err == nil {
		t.Fatal("want error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err=%v, want 500 mentioned", err)
	}
	assertKeyNotInError(t, err)
}

func TestDoJSON_RejectsCrossHostRedirect(t *testing.T) {
	// Server A redirects to Server B; A's host is what we configure, so
	// any redirect to B is cross-host.
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"leak"}`))
	}))
	t.Cleanup(serverB.Close)

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, serverB.URL+"/api/me", http.StatusFound)
	}))
	t.Cleanup(serverA.Close)

	c, err := New(Credentials{BaseURL: serverA.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetMe(context.Background())
	if err == nil {
		t.Fatal("want error for cross-host redirect")
	}
	if !strings.Contains(err.Error(), "cross-host") && !strings.Contains(err.Error(), "redirect") {
		t.Errorf("err=%v, want redirect-rejection", err)
	}
	assertKeyNotInError(t, err)
}

func TestDoJSON_RejectsRedirectChain(t *testing.T) {
	var hops atomic.Int32
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		// Always 302 to same server — infinite loop trips the cap.
		hops.Add(1)
		http.Redirect(w, r, srv.URL+"/api/me", http.StatusFound)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New(Credentials{BaseURL: srv.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetMe(context.Background())
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

func TestDoJSON_HonorsContextTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		// Block until ctx deadline. We sleep longer than the caller's
		// context to ensure the caller's cancel triggers first.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	})
	c, _ := newServerClient(t, mux)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.GetMe(ctx)
	if err == nil {
		t.Fatal("want error for context deadline")
	}
	assertKeyNotInError(t, err)
}

func TestInjectAuth_SetsBearerHeader(t *testing.T) {
	creds := Credentials{BaseURL: "http://x", APIKey: "xyz"}
	req, _ := http.NewRequest(http.MethodGet, "http://x/a", nil)
	creds.injectAuth(req)
	if got := req.Header.Get("Authorization"); got != "Bearer xyz" {
		t.Errorf("Authorization=%q, want Bearer xyz", got)
	}
}

func TestInjectAuth_NoHeaderForEmptyKey(t *testing.T) {
	creds := Credentials{APIKey: ""}
	req, _ := http.NewRequest(http.MethodGet, "http://x/a", nil)
	creds.injectAuth(req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("want no header for empty key; got %q", got)
	}
}

func TestRedactedURL_StripsQueryAndFragment(t *testing.T) {
	u, _ := url.Parse("http://host:1234/a/b?token=secret&p=1#frag")
	got := redactedURL(u)
	if strings.Contains(got, "secret") {
		t.Errorf("redactedURL leaked query: %s", got)
	}
	if strings.Contains(got, "frag") {
		t.Errorf("redactedURL leaked fragment: %s", got)
	}
	if !strings.Contains(got, "/a/b") {
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
	// Validate JSON at serve time so a bad fixture fails loudly.
	var scratch any
	if err := json.Unmarshal(data, &scratch); err != nil {
		t.Fatalf("fixture %s is not valid JSON: %v", name, err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// assertKeyNotInError verifies that the API key never leaks into an
// error's Error() string. Every error path above asserts this —
// the test key is intentionally long + distinctive so a substring
// match is meaningful.
func assertKeyNotInError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, testAPIKey) {
		t.Errorf("API key leaked into error string: %q", msg)
	}
	// "Authorization" as a header name would be a hint that the error
	// formatter is dumping request headers — also disallowed.
	if strings.Contains(msg, "Authorization") {
		t.Errorf("Authorization header name leaked into error string: %q", msg)
	}
}
