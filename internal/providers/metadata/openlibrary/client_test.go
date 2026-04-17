package openlibrary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/providers/metadata"
)

func TestLookupByISBN_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/books" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("bibkeys") != "ISBN:9780441172719" {
			t.Fatalf("unexpected bibkeys %q", r.URL.Query().Get("bibkeys"))
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Shelf/") {
			t.Errorf("User-Agent missing: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "ISBN:9780441172719": {
    "title": "Dune",
    "subtitle": "",
    "authors": [{"name": "Frank Herbert"}],
    "publishers": [{"name": "Ace"}],
    "publish_date": "1990",
    "number_of_pages": 604,
    "identifiers": {"isbn_10": ["0441172717"], "isbn_13": ["9780441172719"]},
    "cover": {"large": "https://covers.example/x-L.jpg"},
    "key": "/books/OL123456M",
    "subjects": [{"name": "Fiction"}, {"name": "Science fiction"}, {"name": "Space"}]
  }
}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	m, err := c.LookupByISBN(context.Background(), "9780441172719")
	if err != nil {
		t.Fatalf("LookupByISBN: %v", err)
	}
	if m.Title != "Dune" {
		t.Errorf("Title: got %q", m.Title)
	}
	if len(m.Authors) != 1 || m.Authors[0] != "Frank Herbert" {
		t.Errorf("Authors: %v", m.Authors)
	}
	if m.Publisher != "Ace" {
		t.Errorf("Publisher: %q", m.Publisher)
	}
	if m.PublishDate != "1990" {
		t.Errorf("PublishDate: %q", m.PublishDate)
	}
	if m.TotalPages == nil || *m.TotalPages != 604 {
		t.Errorf("TotalPages: %v", m.TotalPages)
	}
	if m.ISBN10 != "0441172717" {
		t.Errorf("ISBN10: %q", m.ISBN10)
	}
	if m.ISBN13 != "9780441172719" {
		t.Errorf("ISBN13: %q", m.ISBN13)
	}
	if m.SourceID != "OL123456M" {
		t.Errorf("SourceID: %q", m.SourceID)
	}
	if m.CoverRef != "olid:OL123456M" {
		t.Errorf("CoverRef: %q", m.CoverRef)
	}
	if m.SourceName != "Open Library" {
		t.Errorf("SourceName: %q", m.SourceName)
	}
	// "Fiction" filtered, "Science fiction" + "Space" kept.
	if len(m.Categories) != 2 || m.Categories[0] != "Science fiction" || m.Categories[1] != "Space" {
		t.Errorf("Categories: %v", m.Categories)
	}
}

func TestLookupByISBN_EmptyResponse_ReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.LookupByISBN(context.Background(), "9780000000000")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLookupByISBN_404_ReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, &http.Request{})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.LookupByISBN(context.Background(), "9780000000000")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLookupByISBN_InvalidISBN(t *testing.T) {
	c := newTestClient("http://unused", "http://unused")
	_, err := c.LookupByISBN(context.Background(), "not-an-isbn")
	if err == nil {
		t.Fatal("expected error for invalid ISBN")
	}
}

func TestLookupByISBN_NonJSONContentType_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html>oops</html>`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.LookupByISBN(context.Background(), "9780441172719")
	if err == nil || !strings.Contains(err.Error(), "unexpected content-type") {
		t.Fatalf("expected content-type error, got %v", err)
	}
}

func TestLookupByISBN_OversizedBody_Errors(t *testing.T) {
	big := strings.Repeat("x", jsonMaxBytes+10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"padding":"` + big + `"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.LookupByISBN(context.Background(), "9780441172719")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-cap error, got %v", err)
	}
}

func TestSearch_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "dune herbert" {
			t.Fatalf("unexpected q %q", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "docs": [
    {
      "key": "/works/OL45883W",
      "title": "Dune",
      "author_name": ["Frank Herbert"],
      "first_publish_year": 1965,
      "isbn": ["0441172717", "9780441172719", "not-a-real-isbn"],
      "cover_i": 10521270
    },
    {
      "key": "/works/OL45884W",
      "title": "Dune Messiah",
      "author_name": ["Frank Herbert"],
      "first_publish_year": 1969,
      "isbn": ["0441172733"],
      "cover_i": 0
    },
    {
      "title": ""
    }
  ]
}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	results, err := c.Search(context.Background(), "dune herbert")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (empty-title filtered), got %d", len(results))
	}
	r0 := results[0]
	if r0.Title != "Dune" {
		t.Errorf("Title: %q", r0.Title)
	}
	if r0.PublishYear != "1965" {
		t.Errorf("PublishYear: %q", r0.PublishYear)
	}
	if r0.ISBN != "0441172717" {
		t.Errorf("ISBN: %q", r0.ISBN)
	}
	if r0.CoverRef != "id:10521270" {
		t.Errorf("CoverRef: %q", r0.CoverRef)
	}
	if r0.SourceID != "OL45883W" {
		t.Errorf("SourceID: %q", r0.SourceID)
	}
	// Second entry has cover_i=0 so should fall back to ISBN-based ref.
	r1 := results[1]
	if r1.CoverRef != "isbn:0441172733" {
		t.Errorf("CoverRef fallback: %q", r1.CoverRef)
	}
}

func TestSearch_EmptyQuery_NoHTTPCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called for empty query")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	r, err := c.Search(context.Background(), "   ")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(r) != 0 {
		t.Errorf("expected empty slice, got %d", len(r))
	}
}

func TestFetchCover_ByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b/id/12345-L.jpg" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("default") != "false" {
			t.Fatalf("missing ?default=false")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0, 'x', 'x'})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	img, err := c.FetchCover(context.Background(), "id:12345")
	if err != nil {
		t.Fatalf("FetchCover: %v", err)
	}
	if img.Ext != ".jpg" {
		t.Errorf("Ext: %q", img.Ext)
	}
	if img.ContentType != "image/jpeg" {
		t.Errorf("ContentType: %q", img.ContentType)
	}
	if len(img.Bytes) != 6 {
		t.Errorf("Bytes length: %d", len(img.Bytes))
	}
}

func TestFetchCover_ByOLID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b/olid/OL123456M-L.jpg" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	img, err := c.FetchCover(context.Background(), "olid:OL123456M")
	if err != nil {
		t.Fatalf("FetchCover: %v", err)
	}
	if img.Ext != ".png" {
		t.Errorf("Ext: %q", img.Ext)
	}
}

func TestFetchCover_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, &http.Request{})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.FetchCover(context.Background(), "id:12345")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFetchCover_UnexpectedContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write([]byte("GIF89a"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.FetchCover(context.Background(), "id:12345")
	if err == nil || !strings.Contains(err.Error(), "unexpected cover content-type") {
		t.Fatalf("expected content-type error, got %v", err)
	}
}

func TestFetchCover_InvalidRefs(t *testing.T) {
	c := newTestClient("http://unused", "http://unused")
	refs := []string{
		"",
		":",
		"id:",
		"id:abc",
		"id:" + strings.Repeat("9", 20), // too long
		"olid:notanolid",
		"olid:OLxyzM",
		"olid:OL",
		"isbn:1234",
		"isbn:123456789012", // 12 digits, not 10 or 13
		"unknown:anything",
	}
	for _, ref := range refs {
		_, err := c.FetchCover(context.Background(), ref)
		if err == nil {
			t.Errorf("ref %q should have errored", ref)
		}
	}
}

func TestFetchCover_OversizedImage_Errors(t *testing.T) {
	payload := make([]byte, coverMaxBytes+10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.FetchCover(context.Background(), "id:12345")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-cap error, got %v", err)
	}
}

func TestFetchCover_EmptyBody_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		// Open Library with ?default=false *should* 404 but occasionally
		// returns an empty body instead. We treat empty bodies as
		// not-found so the caller gets a consistent sentinel.
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, srv.URL)
	_, err := c.FetchCover(context.Background(), "id:12345")
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty body, got %v", err)
	}
}

func TestClient_SatisfiesProvider(t *testing.T) {
	var _ metadata.Provider = (*Client)(nil)
}

func TestIsValidISBN(t *testing.T) {
	good := []string{"0441172717", "044117271X", "044117271x", "9780441172719"}
	for _, s := range good {
		if !isValidISBN(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	bad := []string{"", "1234", "12345678901", "abcdefghij", "044117X717"}
	for _, s := range bad {
		if isValidISBN(s) {
			t.Errorf("expected %q invalid", s)
		}
	}
}
