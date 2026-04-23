package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/config"
	"github.com/inarun/Shelf/internal/covers"
	httpserver "github.com/inarun/Shelf/internal/http/server"
	"github.com/inarun/Shelf/internal/index/store"
	sync_ "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/providers/metadata"
)

// fakeProvider is a metadata.Provider that answers from preset tables.
// Lets the integration test exercise the add-book handlers without
// touching the real Open Library.
type fakeProvider struct {
	byISBN   map[string]*metadata.Metadata
	search   []metadata.SearchResult
	covers   map[string]*metadata.CoverImage
	fetchLog []string
}

func (f *fakeProvider) LookupByISBN(_ context.Context, isbn string) (*metadata.Metadata, error) {
	m, ok := f.byISBN[isbn]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return m, nil
}

func (f *fakeProvider) Search(_ context.Context, q string) ([]metadata.SearchResult, error) {
	_ = q
	return f.search, nil
}

func (f *fakeProvider) FetchCover(_ context.Context, ref string) (*metadata.CoverImage, error) {
	f.fetchLog = append(f.fetchLog, ref)
	img, ok := f.covers[ref]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	return img, nil
}

// buildServerWithMetadata stands up the HTTP stack the same way
// buildServer does but also injects a metadata.Provider and a covers
// Cache so the add-book routes can be exercised.
func buildServerWithMetadata(t *testing.T, fp *fakeProvider) (string, string, string, func()) {
	t.Helper()
	root := t.TempDir()
	books := filepath.Join(root, "books")
	backups := filepath.Join(root, "backups")
	coversRoot := filepath.Join(root, "covers")
	for _, d := range []string{books, backups, coversRoot} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(books, "Hyperion by Dan Simmons.md")
	if err := os.WriteFile(path, []byte(hyperion), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	sy := sync_.New(st, books)
	if _, err := sy.FullScan(context.Background()); err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	cache, err := covers.New(coversRoot)
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := httpserver.New(httpserver.Dependencies{
		Config:      &config.Config{Server: config.ServerConfig{Bind: "127.0.0.1", Port: 7744}},
		Store:       st,
		Syncer:      sy,
		Metadata:    fp,
		Covers:      cache,
		BooksAbs:    books,
		BackupsRoot: backups,
		Logger:      logger,
	})
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	cleanup := func() {
		ts.Close()
		_ = st.Close()
	}
	return ts.URL, books, coversRoot, cleanup
}

func TestEndToEnd_AddBookFlow(t *testing.T) {
	// Fake provider returns Foundation for ISBN 9780553293357 and a
	// valid 2-byte JPEG SOI (just enough to satisfy our covers cache —
	// it doesn't inspect image content beyond the content-type).
	pngBytes := []byte{0xff, 0xd8, 0xff, 0xe0, 'x', 'x'}
	eight := 8
	foundation := &metadata.Metadata{
		Title:       "Foundation",
		Authors:     []string{"Isaac Asimov"},
		Publisher:   "Del Rey",
		PublishDate: "1991",
		TotalPages:  &[]int{255}[0],
		ISBN13:      "9780553293357",
		Categories:  []string{"Science fiction"},
		CoverRef:    "olid:OL7602362M",
		SourceName:  "Open Library",
		SourceID:    "OL7602362M",
	}
	_ = eight
	fp := &fakeProvider{
		byISBN: map[string]*metadata.Metadata{
			"9780553293357": foundation,
		},
		search: []metadata.SearchResult{
			{Title: "Foundation", Authors: []string{"Isaac Asimov"},
				PublishYear: "1951", ISBN: "9780553293357",
				CoverRef: "olid:OL7602362M", SourceID: "OL123W"},
		},
		covers: map[string]*metadata.CoverImage{
			"olid:OL7602362M": {Bytes: pngBytes, ContentType: "image/jpeg", Ext: ".jpg"},
		},
	}

	base, books, coversRoot, cleanup := buildServerWithMetadata(t, fp)
	defer cleanup()

	jar, _ := newCookieJar()
	client := &http.Client{Jar: jar}

	// 1. GET /add → HTML with CSRF meta.
	resp := doReq(t, client, http.MethodGet, base+"/add", nil, nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /add status = %d", resp.StatusCode)
	}
	csrf := extractCSRF(t, string(body))
	if !strings.Contains(string(body), "By ISBN") {
		t.Errorf("/add body missing ISBN form")
	}

	// 2. POST /api/add/lookup — JSON Metadata.
	resp = doReq(t, client, http.MethodPost, base+"/api/add/lookup",
		strings.NewReader(`{"isbn":"9780553293357"}`),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
		})
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("lookup status = %d body=%s", resp.StatusCode, body)
	}
	var lookupResp struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(body, &lookupResp); err != nil {
		t.Fatal(err)
	}
	if lookupResp.Metadata["title"] != "Foundation" {
		t.Errorf("title = %v", lookupResp.Metadata["title"])
	}
	coverRef, _ := lookupResp.Metadata["cover_ref"].(string)
	if coverRef == "" {
		t.Fatal("metadata missing cover_ref")
	}

	// 3. POST /api/add/cover — triggers a fetch, stores in cache.
	resp = doReq(t, client, http.MethodPost, base+"/api/add/cover",
		strings.NewReader(`{"ref":"`+coverRef+`"}`),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
		})
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cover status = %d body=%s", resp.StatusCode, body)
	}
	var coverResp struct {
		Cover string `json:"cover"`
	}
	if err := json.Unmarshal(body, &coverResp); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(coverResp.Cover, "/covers/") || !strings.HasSuffix(coverResp.Cover, ".jpg") {
		t.Errorf("cover path = %q", coverResp.Cover)
	}

	// Hitting the same cover ref a second time must be idempotent and
	// not call the provider again (cache hit path).
	initialFetches := len(fp.fetchLog)
	resp = doReq(t, client, http.MethodPost, base+"/api/add/cover",
		strings.NewReader(`{"ref":"`+coverRef+`"}`),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
		})
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(fp.fetchLog) != initialFetches {
		t.Errorf("second /api/add/cover must hit cache; fetchLog grew from %d to %d",
			initialFetches, len(fp.fetchLog))
	}

	// 4. POST /api/add/create — writes the note on disk.
	createBody := map[string]any{
		"title":        "Foundation",
		"authors":      []string{"Isaac Asimov"},
		"publisher":    "Del Rey",
		"publish_date": "1991",
		"total_pages":  255,
		"isbn":         "9780553293357",
		"categories":   []string{"Science fiction"},
		"cover":        coverResp.Cover,
	}
	createJSON, _ := json.Marshal(createBody)
	resp = doReq(t, client, http.MethodPost, base+"/api/add/create",
		strings.NewReader(string(createJSON)),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
		})
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", resp.StatusCode, body)
	}
	var createResp struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(body, &createResp); err != nil {
		t.Fatal(err)
	}
	if createResp.Filename != "Foundation by Isaac Asimov.md" {
		t.Errorf("filename = %q", createResp.Filename)
	}

	// Disk must now contain the new note with the cover field.
	noteBytes, err := os.ReadFile(filepath.Join(books, createResp.Filename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(noteBytes), "title: Foundation") {
		t.Errorf("note missing title; content:\n%s", noteBytes)
	}
	if !strings.Contains(string(noteBytes), coverResp.Cover) {
		t.Errorf("note missing cover ref %q; content:\n%s", coverResp.Cover, noteBytes)
	}

	// 5. GET the served cover image → 200, image/jpeg.
	resp = doReq(t, client, http.MethodGet, base+coverResp.Cover, nil, nil)
	imgBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", coverResp.Cover, resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("cover Content-Type = %q", got)
	}
	if len(imgBody) != len(pngBytes) {
		t.Errorf("cover bytes length = %d, want %d", len(imgBody), len(pngBytes))
	}

	// 6. GET /library → includes the new book.
	resp = doReq(t, client, http.MethodGet, base+"/library", nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "Foundation") {
		t.Errorf("library page missing Foundation")
	}

	// 7. GET /stats → renders with our one finished, one unread book.
	resp = doReq(t, client, http.MethodGet, base+"/stats", nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "lifetime reads") {
		t.Errorf("stats page missing summary")
	}

	// Sanity-check on disk: the cache file actually exists and is the
	// provider's bytes.
	diskCover := filepath.Join(coversRoot, strings.TrimPrefix(coverResp.Cover, "/covers/"))
	got, err := os.ReadFile(diskCover)
	if err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
	if string(got) != string(pngBytes) {
		t.Errorf("cache bytes mismatch")
	}
}

func TestEndToEnd_AddBookLookupNotFound(t *testing.T) {
	fp := &fakeProvider{byISBN: map[string]*metadata.Metadata{}}
	base, _, _, cleanup := buildServerWithMetadata(t, fp)
	defer cleanup()

	jar, _ := newCookieJar()
	client := &http.Client{Jar: jar}

	// Mint CSRF.
	resp := doReq(t, client, http.MethodGet, base+"/add", nil, nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	csrf := extractCSRF(t, string(body))

	resp = doReq(t, client, http.MethodPost, base+"/api/add/lookup",
		strings.NewReader(`{"isbn":"9780000000000"}`),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
		})
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("not-found ISBN: status = %d, want 404", resp.StatusCode)
	}
}

func TestEndToEnd_ServeCover_InvalidFilename(t *testing.T) {
	fp := &fakeProvider{}
	base, _, _, cleanup := buildServerWithMetadata(t, fp)
	defer cleanup()

	client := &http.Client{}
	// Path traversal / garbage filenames must 404 — the handler goes
	// through Cache.AbsPath which enforces the sha256+ext allowlist.
	for _, bad := range []string{"..%2Fetc%2Fpasswd", "foo.jpg", strings.Repeat("a", 63) + ".jpg"} {
		resp := doReq(t, client, http.MethodGet, base+"/covers/"+bad, nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("cover %q: status = %d, want 404", bad, resp.StatusCode)
		}
	}
}

// Ensure the provider interface is satisfied at compile time.
var _ metadata.Provider = (*fakeProvider)(nil)
var _ = errors.New
