package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/config"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
)

const fixtureHyperion = `---
title: Hyperion
authors:
  - Dan Simmons
rating: 4
status: reading
read_count: 0
---
# Hyperion

Rating — 4/5

## Notes

Dense first half.

## Reading Timeline

- 2025-03-09 — Started
`

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	books := filepath.Join(root, "books")
	backups := filepath.Join(root, "backups")
	for _, d := range []string{books, backups} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(books, "Hyperion by Dan Simmons.md")
	if err := os.WriteFile(path, []byte(fixtureHyperion), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sy := sync.New(st, books)
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Server: config.ServerConfig{Bind: "127.0.0.1", Port: 7744},
	}
	s, err := New(Dependencies{
		Config:      cfg,
		Store:       st,
		Syncer:      sy,
		BooksAbs:    books,
		BackupsRoot: backups,
		Logger:      logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, books
}

func TestLibraryIsServedWithSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	req.Host = "127.0.0.1:7744"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /library: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Hyperion") {
		t.Errorf("missing book title in library body")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("CSP missing")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "shelf_session" {
		t.Errorf("expected shelf_session cookie, got %v", cookies)
	}
}

func TestRootRedirectsToLibrary(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:7744"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	if rec.Header().Get("Location") != "/library" {
		t.Errorf("Location = %q, want /library", rec.Header().Get("Location"))
	}
}

func TestHostRejectsEvilHost(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	req.Host = "evil.example.com:7744"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421", rec.Code)
	}
}

func TestPatchWithoutCSRFIs403(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/books/Hyperion%20by%20Dan%20Simmons.md",
		strings.NewReader(`{"rating": 5}`))
	req.Host = "127.0.0.1:7744"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	req.Host = "127.0.0.1:7744"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "csrf-token") {
		t.Errorf("app.js content missing; got %d bytes", rec.Body.Len())
	}
}

func TestUnknownRouteJSON404(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	req.Host = "127.0.0.1:7744"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("expected JSON 404 envelope; got %s", rec.Body.String())
	}
}

func TestKeysAreUniquePerProcess(t *testing.T) {
	k1, err := NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	if k1.HMAC == k2.HMAC {
		t.Errorf("HMAC keys should differ across invocations")
	}
}
