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
	"time"

	"github.com/inarun/Shelf/internal/http/templates"
	"github.com/inarun/Shelf/internal/index/store"
	sync_ "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/body"
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

// TestComposeTimeline exercises the pairing logic end-to-end: two
// finished pairs produce two "finished" entries; a trailing started
// with no finished pairs with the current Status; legacy gaps earlier
// in the sequence stay stateless.
func TestComposeTimeline(t *testing.T) {
	t.Run("paused trail", func(t *testing.T) {
		got := composeTimeline(
			[]string{"2024-01-10", "2025-01-15", "2025-11-20"},
			[]string{"2024-03-20", "2025-03-28"},
			"paused",
		)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if got[0].State != "finished" || got[1].State != "finished" {
			t.Errorf("first two entries must be finished: %+v", got)
		}
		if got[2].State != "paused" {
			t.Errorf("trailing unfinished entry must take Status 'paused': %+v", got[2])
		}
		if got[2].Label != "Paused, last started 2025-11-20" {
			t.Errorf("label = %q", got[2].Label)
		}
	})
	t.Run("reading with no prior", func(t *testing.T) {
		got := composeTimeline([]string{"2025-06-01"}, nil, "reading")
		if len(got) != 1 || got[0].State != "reading" {
			t.Errorf("reading-only: %+v", got)
		}
		if got[0].Label != "Reading since 2025-06-01" {
			t.Errorf("label = %q", got[0].Label)
		}
	})
	t.Run("finished terminal", func(t *testing.T) {
		got := composeTimeline([]string{"2025-01-10"}, []string{"2025-02-20"}, "finished")
		if len(got) != 1 || got[0].State != "finished" {
			t.Errorf("finished: %+v", got)
		}
	})
	t.Run("dnf terminal", func(t *testing.T) {
		got := composeTimeline([]string{"2025-05-01"}, nil, "dnf")
		if got[0].State != "dnf" || got[0].Label != "Stopped reading, last started 2025-05-01" {
			t.Errorf("dnf: %+v", got[0])
		}
	})
	t.Run("nothing", func(t *testing.T) {
		if got := composeTimeline(nil, nil, ""); got != nil {
			t.Errorf("want nil, got %+v", got)
		}
	})
	t.Run("legacy gap earlier in sequence", func(t *testing.T) {
		// started[0] without finished[0] but NOT the trailing pair →
		// legacy historical entry, no State inferred from current Status.
		got := composeTimeline(
			[]string{"2023-06-01", "2024-01-01"},
			[]string{"", "2024-04-01"},
			"finished",
		)
		if len(got) != 2 {
			t.Fatalf("len = %d", len(got))
		}
		if got[0].State != "" {
			t.Errorf("historical gap must have empty State, got %q", got[0].State)
		}
		if got[1].State != "finished" {
			t.Errorf("terminal pair state = %q", got[1].State)
		}
	})
}

// TestMarkAudioSources_MatchesByDate exercises the audio-badge detection
// helper that tags TimelineEntry rows whose date matches a body-timeline
// event containing "(Audiobookshelf)". This is the logic
// `TestTimelineShowsAudioBadgeOnABEntries` (template side) can't cover
// end-to-end — that test feeds an already-tagged slice; this one
// verifies the tagger itself.
func TestMarkAudioSources_MatchesByDate(t *testing.T) {
	mkDate := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad date %q: %v", s, err)
		}
		return d
	}

	t.Run("finished-line match tags finished entry", func(t *testing.T) {
		entries := []TimelineEntry{
			{Started: "2025-01-10", Finished: "2025-02-14", State: "finished"},
		}
		events := []body.TimelineEvent{
			{Date: mkDate("2025-02-14"), Text: "Finished listening (Audiobookshelf)"},
		}
		got := markAudioSources(entries, events)
		if got[0].Source != "audiobookshelf" {
			t.Errorf("Source = %q, want audiobookshelf", got[0].Source)
		}
	})

	t.Run("started-line match tags in-progress entry", func(t *testing.T) {
		entries := []TimelineEntry{
			{Started: "2025-03-01", Finished: "", State: "reading"},
		}
		events := []body.TimelineEvent{
			{Date: mkDate("2025-03-01"), Text: "Started listening (Audiobookshelf)"},
		}
		got := markAudioSources(entries, events)
		if got[0].Source != "audiobookshelf" {
			t.Errorf("Source = %q, want audiobookshelf", got[0].Source)
		}
	})

	t.Run("no match leaves Source empty", func(t *testing.T) {
		entries := []TimelineEntry{
			{Started: "2025-01-10", Finished: "2025-02-14", State: "finished"},
		}
		events := []body.TimelineEvent{
			// Different date, so no match — even though the text has the marker.
			{Date: mkDate("2099-01-01"), Text: "Finished listening (Audiobookshelf)"},
			// Same date but no marker — vault-authored line.
			{Date: mkDate("2025-02-14"), Text: "Finished (Kindle)"},
		}
		got := markAudioSources(entries, events)
		if got[0].Source != "" {
			t.Errorf("Source = %q, want empty", got[0].Source)
		}
	})

	t.Run("no body events leaves all entries vault-origin", func(t *testing.T) {
		entries := []TimelineEntry{
			{Started: "2025-01-10", Finished: "2025-02-14", State: "finished"},
			{Started: "2025-03-01", Finished: "", State: "reading"},
		}
		got := markAudioSources(entries, nil)
		for i, e := range got {
			if e.Source != "" {
				t.Errorf("entry %d Source = %q, want empty", i, e.Source)
			}
		}
	})

	t.Run("zero-date event is ignored", func(t *testing.T) {
		// Malformed body lines parse to TimelineEvent{Date: time.Time{}, Raw: ...}
		// — the helper must skip them, not match every entry.
		entries := []TimelineEntry{
			{Started: "2025-01-10", Finished: "2025-02-14", State: "finished"},
		}
		events := []body.TimelineEvent{
			{Text: "Finished listening (Audiobookshelf)"}, // zero Date
		}
		got := markAudioSources(entries, events)
		if got[0].Source != "" {
			t.Errorf("Source = %q, want empty (zero-date event)", got[0].Source)
		}
	})

	t.Run("mixed entries tag only the AB one", func(t *testing.T) {
		entries := []TimelineEntry{
			{Started: "2024-05-01", Finished: "2024-06-03", State: "finished"}, // vault
			{Started: "2025-01-10", Finished: "2025-02-14", State: "finished"}, // AB
		}
		events := []body.TimelineEvent{
			{Date: mkDate("2024-06-03"), Text: "Finished (Kindle)"},
			{Date: mkDate("2025-02-14"), Text: "Finished listening (Audiobookshelf)"},
		}
		got := markAudioSources(entries, events)
		if got[0].Source != "" {
			t.Errorf("vault entry Source = %q, want empty", got[0].Source)
		}
		if got[1].Source != "audiobookshelf" {
			t.Errorf("AB entry Source = %q, want audiobookshelf", got[1].Source)
		}
	})
}
