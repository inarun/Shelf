package audiobookshelf

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// newVaultEnv sets up a temp Books folder + SQLite-backed store +
// Syncer. Notes written through writeNote are immediately discoverable
// by a FullScan.
func newVaultEnv(t *testing.T) (booksAbs, backupsRoot string, s *store.Store, sy *sync.Syncer) {
	t.Helper()
	dir := t.TempDir()
	booksAbs = filepath.Join(dir, "Books")
	backupsRoot = filepath.Join(dir, "backups")
	if err := os.MkdirAll(booksAbs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	s = newTestStore(t)
	sy = sync.New(s, booksAbs)
	return
}

// newFM returns a minimal frontmatter for a book note — title +
// authors set, status unread, nothing else. Test helper shared across
// plan_test.go and apply_test.go.
func newFM(title, author string) *frontmatter.Frontmatter {
	fm := frontmatter.NewEmpty()
	fm.SetTitle(title)
	fm.SetAuthors([]string{author})
	_ = fm.SetStatus("unread")
	return fm
}

func writeNote(t *testing.T, booksAbs, filename string, fm *frontmatter.Frontmatter, bd *body.Body) {
	t.Helper()
	if bd == nil {
		bd = &body.Body{}
		bd.SetTitle(fm.Title())
	}
	data, err := fm.Serialize(bd.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if err := atomic.Write(filepath.Join(booksAbs, filename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedHyperion writes a barebones unread note for Hyperion and indexes
// it. Returns the filename under booksAbs.
func seedHyperion(t *testing.T, booksAbs string, sy *sync.Syncer, withISBN bool) string {
	t.Helper()
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	if withISBN {
		fm.SetISBN("9780553283686")
	}
	_ = fm.SetStatus("unread")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, nil)
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return "Hyperion by Dan Simmons.md"
}

func abItem(id, title, author, isbn string, finished bool) LibraryItem {
	return LibraryItem{
		ID:         id,
		IsFinished: finished,
		LastUpdate: 1_700_000_000_000,
		Media: LibraryMedia{Metadata: LibraryMediaMetadata{
			Title: title, AuthorName: author, ISBN: isbn,
		}},
	}
}

func TestBuildPlan_UnmatchedItem(t *testing.T) {
	booksAbs, _, s, sy := newVaultEnv(t)
	// Empty vault — nothing to match.
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-unmatched", "Totally Unknown Book", "Nobody", "", true)}
	p, err := BuildPlan(context.Background(), items, nil, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Unmatched) != 1 {
		t.Fatalf("want 1 unmatched, got %+v", p)
	}
	if p.Unmatched[0].ABItemID != "ab-unmatched" {
		t.Errorf("ab_item_id = %q", p.Unmatched[0].ABItemID)
	}
}

func TestBuildPlan_MatchWithGapToFill(t *testing.T) {
	booksAbs, _, s, sy := newVaultEnv(t)
	seedHyperion(t, booksAbs, sy, true)
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-1", "Hyperion", "Dan Simmons", "978-0-553-28368-6", true)}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-1", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillUpdate) != 1 {
		t.Fatalf("want 1 WillUpdate, got %+v", p)
	}
	u := p.WillUpdate[0]
	if u.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("filename = %q", u.Filename)
	}
	if len(u.NewEntries) != 1 {
		t.Fatalf("want 1 NewEntry, got %+v", u.NewEntries)
	}
	if u.NewEntries[0].ExternalID != "ab-1" {
		t.Errorf("external id = %q", u.NewEntries[0].ExternalID)
	}
}

func TestBuildPlan_MatchWithNoGap(t *testing.T) {
	// Vault already has a finished entry on the exact day AB reports —
	// Merge drops the external entry as an overlap; plan is SkipEntry.
	booksAbs, _, s, sy := newVaultEnv(t)
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	fm.SetISBN("9780553283686")
	fm.AppendStarted(time.UnixMilli(1_700_000_000_000).UTC())
	fm.AppendFinished(time.UnixMilli(1_700_100_000_000).UTC())
	_ = fm.SetStatus("finished")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, nil)
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-1", "Hyperion", "Dan Simmons", "9780553283686", true)}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-1", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillSkip) != 1 {
		t.Fatalf("want 1 WillSkip (vault covers external), got %+v", p)
	}
	if len(p.WillUpdate) != 0 {
		t.Errorf("no updates expected, got %d", len(p.WillUpdate))
	}
}

func TestBuildPlan_AmbiguousMatchLandsInConflicts(t *testing.T) {
	booksAbs, _, s, sy := newVaultEnv(t)
	// Vault title "The Expanse" authored by "James Corey"
	fm := frontmatter.NewEmpty()
	fm.SetTitle("The Expanse")
	fm.SetAuthors([]string{"James Corey"})
	_ = fm.SetStatus("unread")
	writeNote(t, booksAbs, "The Expanse by James Corey.md", fm, nil)
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	// AB item: same title, different author surname → NeedsUserDecision
	items := []LibraryItem{abItem("ab-e", "The Expanse", "James Otherly", "", true)}
	p, err := BuildPlan(context.Background(), items, nil, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) != 1 {
		t.Fatalf("want 1 conflict, got %+v", p)
	}
	if !p.Conflicts[0].NeedsUserDecision {
		t.Error("conflict should require user decision")
	}
}

func TestBuildPlan_NewEntriesOrderedByStart(t *testing.T) {
	// Two matched items; BuildPlan sorts WillUpdate by filename so the
	// JSON output is deterministic.
	booksAbs, _, s, sy := newVaultEnv(t)
	seedHyperion(t, booksAbs, sy, true)

	fmX := frontmatter.NewEmpty()
	fmX.SetTitle("Xenocide")
	fmX.SetAuthors([]string{"Orson Scott Card"})
	fmX.SetISBN("9780812509250")
	_ = fmX.SetStatus("unread")
	writeNote(t, booksAbs, "Xenocide by Orson Scott Card.md", fmX, nil)
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{
		abItem("ab-x", "Xenocide", "Orson Scott Card", "9780812509250", true),
		abItem("ab-h", "Hyperion", "Dan Simmons", "9780553283686", true),
	}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-h", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
		{LibraryItemID: "ab-x", StartedAt: 1_700_200_000_000, UpdatedAt: 1_700_300_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillUpdate) != 2 {
		t.Fatalf("want 2 updates, got %+v", p)
	}
	if p.WillUpdate[0].Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("filename[0] = %q (expected alphabetical sort)", p.WillUpdate[0].Filename)
	}
	if p.WillUpdate[1].Filename != "Xenocide by Orson Scott Card.md" {
		t.Errorf("filename[1] = %q", p.WillUpdate[1].Filename)
	}
}
