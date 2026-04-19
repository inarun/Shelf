package handlers

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

	"github.com/inarun/Shelf/internal/http/templates"
	"github.com/inarun/Shelf/internal/index/store"
	sync_ "github.com/inarun/Shelf/internal/index/sync"
)

const hyperionFixture = `---
title: Hyperion
authors:
  - Dan Simmons
categories:
  - science-fiction
rating: 4
status: reading
started:
  - 2025-03-09
finished: []
read_count: 0
series: Hyperion Cantos
series_index: 1
---
# Hyperion

Rating — 4/5

## Notes

Dense first half but worth it.

## Reading Timeline

- 2025-03-09 — Started (Kindle)
`

func seedDeps(t *testing.T) (*Dependencies, string) {
	t.Helper()
	root := t.TempDir()
	books := filepath.Join(root, "books")
	backups := filepath.Join(root, "backups")
	for _, dir := range []string{books, backups} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(books, "Hyperion by Dan Simmons.md")
	if err := os.WriteFile(path, []byte(hyperionFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sy := sync_.New(st, books)
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	tmpl, err := templates.Parse()
	if err != nil {
		t.Fatal(err)
	}
	return &Dependencies{
		Store:       st,
		Syncer:      sy,
		BooksAbs:    books,
		BackupsRoot: filepath.Join(root, "backups"),
		DataDir:     root,
		Templates:   tmpl,
		HMACKey:     []byte("abcdefghijklmnopqrstuvwxyzABCDEF"),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, books
}

func TestLibraryIndexRedirects(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.LibraryIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want 302", rec.Code)
	}
	if rec.Header().Get("Location") != "/library" {
		t.Errorf("Location = %q, want /library", rec.Header().Get("Location"))
	}
}

func TestLibraryViewRendersSeededBook(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.LibraryView(rec, httptest.NewRequest(http.MethodGet, "/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Hyperion", "Dan Simmons", "status-reading", `<meta name="csrf-token"`} {
		if !strings.Contains(body, want) {
			t.Errorf("library body missing %q", want)
		}
	}
}

func TestLibraryViewStatusFilter(t *testing.T) {
	d, _ := seedDeps(t)
	// filter to finished — the seed is 'reading', so should be empty.
	rec := httptest.NewRecorder()
	d.LibraryView(rec, httptest.NewRequest(http.MethodGet, "/library?status=finished", nil))
	if !strings.Contains(rec.Body.String(), "empty-state__icon") {
		t.Errorf("expected empty-state illustration with status=finished filter; body:\n%s", rec.Body.String())
	}
}

func TestLibraryViewDropsInvalidStatusFilter(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.LibraryView(rec, httptest.NewRequest(http.MethodGet, "/library?status=malformed", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("invalid filter should still 200; got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Hyperion") {
		t.Errorf("invalid filter should show all books; body:\n%s", rec.Body.String())
	}
}

func TestBookDetailRendersReviewAndTimeline(t *testing.T) {
	d, _ := seedDeps(t)
	req := httptest.NewRequest(http.MethodGet, "/books/Hyperion%20by%20Dan%20Simmons.md", nil)
	req.SetPathValue("filename", "Hyperion%20by%20Dan%20Simmons.md")
	rec := httptest.NewRecorder()
	d.BookDetail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Hyperion", "Dan Simmons", "Dense first half", "2025-03-09", "Hyperion Cantos"} {
		if !strings.Contains(body, want) {
			t.Errorf("book-detail body missing %q", want)
		}
	}
}

func TestBookDetail404(t *testing.T) {
	d, _ := seedDeps(t)
	req := httptest.NewRequest(http.MethodGet, "/books/Nonexistent.md", nil)
	req.SetPathValue("filename", "Nonexistent.md")
	rec := httptest.NewRecorder()
	d.BookDetail(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestBookDetail400OnTraversal(t *testing.T) {
	d, _ := seedDeps(t)
	req := httptest.NewRequest(http.MethodGet, "/books/..%2Fetc.md", nil)
	req.SetPathValue("filename", "..%2Fetc.md")
	rec := httptest.NewRecorder()
	d.BookDetail(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestImportPageRenders(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.ImportPage(rec, httptest.NewRequest(http.MethodGet, "/import", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Import from Goodreads") {
		t.Errorf("import page missing expected heading")
	}
}

func TestNotFoundHandlerAPIvsHTML(t *testing.T) {
	d, _ := seedDeps(t)
	// JSON for /api/ paths
	rec := httptest.NewRecorder()
	d.NotFoundHandler(rec, httptest.NewRequest(http.MethodGet, "/api/unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("api: status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("api: expected JSON error envelope; got %s", rec.Body.String())
	}
	// HTML for other paths
	rec2 := httptest.NewRecorder()
	d.NotFoundHandler(rec2, httptest.NewRequest(http.MethodGet, "/some-page", nil))
	if !strings.Contains(rec2.Body.String(), "<!doctype") {
		t.Errorf("html: expected HTML body; got %s", rec2.Body.String())
	}
}

func TestHealth(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.Health(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != HealthSignature {
		t.Errorf("healthz: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(HealthSignature, "shelf") {
		t.Errorf("HealthSignature must contain 'shelf' so the single-instance probe can match; got %q", HealthSignature)
	}
}
