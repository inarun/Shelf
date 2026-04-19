package goodreads

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
)

// writeNote composes a note with the provided frontmatter setters +
// optional Notes body text, serializes, and writes atomically to the
// books directory. Returns the filename under booksAbs.
func writeNote(t *testing.T, booksAbs, filename string, fm *frontmatter.Frontmatter, notesText string) string {
	t.Helper()
	bd := &body.Body{}
	bd.SetTitle(fm.Title())
	if notesText != "" {
		bd.SetNotes(notesText)
	}
	data, err := fm.Serialize(bd.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	fullPath := filepath.Join(booksAbs, filename)
	if err := atomic.Write(fullPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return fullPath
}

func setupPlanEnv(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	booksAbs := filepath.Join(dir, "Books")
	if err := os.MkdirAll(booksAbs, 0o700); err != nil {
		t.Fatal(err)
	}
	s := openResolverStore(t)
	return booksAbs, s
}

func TestBuildPlan_WillCreate(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	rv, _ := NewResolver(context.Background(), s)

	records := []Record{{
		RowNum:         1,
		Title:          "Hyperion",
		Author:         "Dan Simmons",
		Authors:        []string{"Dan Simmons"},
		ISBN13:         "9780553283686",
		MyRating:       5,
		ExclusiveShelf: "read",
		Status:         "finished",
	}}
	p, err := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillCreate) != 1 {
		t.Fatalf("expected 1 create entry; got %d", len(p.WillCreate))
	}
	if p.WillCreate[0].Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("filename got %q", p.WillCreate[0].Filename)
	}
}

func TestBuildPlan_WillUpdate_GapFillOnly(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	// Seed a note with partial metadata.
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	// Index it.
	_, err := s.UpsertBook(context.Background(), store.BookRow{
		Filename:      "Hyperion by Dan Simmons.md",
		CanonicalName: true,
		Title:         "Hyperion",
		Authors:       []string{"Dan Simmons"},
		ISBN:          "",
		Status:        "unread",
		SizeBytes:     1,
		MtimeNanos:    1,
		IndexedAtUnix: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		RowNum:         1,
		Title:          "Hyperion",
		Author:         "Dan Simmons",
		Authors:        []string{"Dan Simmons"},
		ISBN13:         "9780553283686",
		Publisher:      "Bantam",
		YearPublished:  "1989",
		MyRating:       5,
		ExclusiveShelf: "read",
		Status:         "finished",
		DateRead:       timeP(t, "2025-04-02"),
	}}
	p, err := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillUpdate) != 1 {
		t.Fatalf("expected update; got %+v", p)
	}
	got := p.WillUpdate[0]
	// Must include status, rating, isbn, publisher, publish, finished, read_count.
	fields := map[string]bool{}
	for _, c := range got.Changes {
		fields[c.Field] = true
	}
	for _, want := range []string{
		frontmatter.KeyStatus, frontmatter.KeyRating, frontmatter.KeyISBN,
		frontmatter.KeyPublisher, frontmatter.KeyPublish, frontmatter.KeyFinished,
		frontmatter.KeyReadCount,
	} {
		if !fields[want] {
			t.Errorf("expected change for %q; got changes: %+v", want, got.Changes)
		}
	}
}

func TestBuildPlan_SkipsFullyPopulated(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	fm.SetISBN("9780553283686")
	fm.SetPublisher("Bantam")
	fm.SetPublish("1989")
	_ = fm.SetStatus("finished")
	over := 5.0
	_ = fm.SetRating(&frontmatter.Rating{Overall: &over})
	fm.AppendFinished(*timeP(t, "2025-04-02"))
	fm.SetReadCount(1)
	fm.SetCategories([]string{"sci-fi"})
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	_, _ = s.UpsertBook(context.Background(), store.BookRow{
		Filename: "Hyperion by Dan Simmons.md", CanonicalName: true,
		Title: "Hyperion", Authors: []string{"Dan Simmons"}, ISBN: "9780553283686",
		Status: "finished", SizeBytes: 1, MtimeNanos: 1, IndexedAtUnix: 1,
	})

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		Title: "Hyperion", Author: "Dan Simmons", Authors: []string{"Dan Simmons"},
		ISBN13: "9780553283686", Publisher: "Bantam", YearPublished: "1989",
		MyRating: 5, ExclusiveShelf: "read", Status: "finished",
		DateRead:    timeP(t, "2025-04-02"),
		Bookshelves: []string{"sci-fi"},
	}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if len(p.WillSkip) != 1 {
		t.Fatalf("expected skip; got %+v", p)
	}
	if len(p.WillUpdate) != 0 {
		t.Errorf("expected 0 updates; got %+v", p.WillUpdate)
	}
}

func TestBuildPlan_ConflictOnEmptyIdentity(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{RowNum: 1, Title: "", Authors: nil}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if len(p.Conflicts) != 1 {
		t.Fatalf("expected conflict; got %+v", p)
	}
}

func TestBuildPlan_PrecedenceStatusUnreadIsGap(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	_ = fm.SetStatus("unread")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	_, _ = s.UpsertBook(context.Background(), store.BookRow{
		Filename: "Hyperion by Dan Simmons.md", CanonicalName: true,
		Title: "Hyperion", Authors: []string{"Dan Simmons"},
		Status: "unread", SizeBytes: 1, MtimeNanos: 1, IndexedAtUnix: 1,
	})

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		Title: "Hyperion", Author: "Dan Simmons", Authors: []string{"Dan Simmons"},
		ExclusiveShelf: "read", Status: "finished",
		DateRead: timeP(t, "2025-04-02"),
	}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if len(p.WillUpdate) != 1 {
		t.Fatalf("expected update (status: unread is a gap); got %+v", p)
	}
	found := false
	for _, c := range p.WillUpdate[0].Changes {
		if c.Field == frontmatter.KeyStatus && c.New == "finished" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected status change to finished; got %+v", p.WillUpdate[0].Changes)
	}
}

func TestBuildPlan_PrecedenceStatusNonDefaultPreserved(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	_ = fm.SetStatus("reading")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	_, _ = s.UpsertBook(context.Background(), store.BookRow{
		Filename: "Hyperion by Dan Simmons.md", CanonicalName: true,
		Title: "Hyperion", Authors: []string{"Dan Simmons"},
		Status: "reading", SizeBytes: 1, MtimeNanos: 1, IndexedAtUnix: 1,
	})

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		Title: "Hyperion", Author: "Dan Simmons", Authors: []string{"Dan Simmons"},
		ExclusiveShelf: "read", Status: "finished",
		DateRead: timeP(t, "2025-04-02"),
	}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	// Status should NOT be in the change list because user explicitly
	// set it to "reading" (not the template default).
	if len(p.WillUpdate) != 1 {
		t.Fatalf("expected update (other gaps); got %+v", p)
	}
	for _, c := range p.WillUpdate[0].Changes {
		if c.Field == frontmatter.KeyStatus {
			t.Errorf("should not propose status change; got %+v", c)
		}
	}
}

func TestBuildPlan_DeterministicSortByFilename(t *testing.T) {
	booksAbs, s := setupPlanEnv(t)
	rv, _ := NewResolver(context.Background(), s)
	records := []Record{
		{RowNum: 2, Title: "Zebra", Author: "B", Authors: []string{"B"}},
		{RowNum: 1, Title: "Apple", Author: "A", Authors: []string{"A"}},
	}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if len(p.WillCreate) != 2 {
		t.Fatalf("expected 2 creates; got %d", len(p.WillCreate))
	}
	if p.WillCreate[0].Filename > p.WillCreate[1].Filename {
		t.Errorf("creates not sorted: %v", p.WillCreate)
	}
}

// timeP parses an ISO date into a *time.Time for table tests.
func timeP(t *testing.T, s string) *time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return &tm
}

// Silence unused-import warnings when certain tests are not compiled.
var _ = note.Read
