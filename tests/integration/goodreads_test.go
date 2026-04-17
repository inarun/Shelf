package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/providers/reading/goodreads"
)

func TestGoodreadsImport_EndToEnd_CreateAndReport(t *testing.T) {
	booksAbs, s, syncer := setup(t)
	backupsRoot := filepath.Join(filepath.Dir(booksAbs), "backups")
	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	csvText := `Book Id,Title,Author,ISBN,ISBN13,My Rating,Exclusive Shelf,Date Read,Date Added,My Review,Publisher,Year Published,Number of Pages,Bookshelves
1,"Hyperion","Dan Simmons","=""0553283685""","=""9780553283686""",5,read,2025/04/02,2024/12/15,"Loved the structure.","Bantam",1989,482,"sci-fi,favorites"
2,"Project Hail Mary","Andy Weir","","=""9780593135204""",0,currently-reading,,2025/01/05,,"Ballantine",2021,476,"sci-fi"
`
	records, err := goodreads.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatalf("CSV parse: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records; got %d", len(records))
	}

	rv, err := goodreads.NewResolver(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	plan, err := goodreads.BuildPlan(context.Background(), records, rv, booksAbs, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.WillCreate) != 2 {
		t.Fatalf("expected 2 creates; got %+v", plan)
	}

	// Dry-run JSON shape.
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"will_create":[`) {
		t.Errorf("JSON missing will_create; got %s", raw)
	}
	if !strings.Contains(string(raw), `"conflicts":[]`) {
		t.Errorf("empty conflicts should render as []; got %s", raw)
	}

	report, err := goodreads.Apply(context.Background(), plan, booksAbs, goodreads.ApplyOptions{
		Syncer: syncer, ImportStamp: stamp, BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Created) != 2 {
		t.Fatalf("expected 2 created; got %+v", report)
	}
	if report.BackupRoot == "" {
		t.Fatal("expected backup root")
	}

	// Hyperion file: frontmatter + blockquoted review.
	hData, err := os.ReadFile(filepath.Join(booksAbs, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	h := string(hData)
	if !strings.Contains(h, "isbn: \"9780553283686\"") && !strings.Contains(h, "isbn: '9780553283686'") && !strings.Contains(h, "isbn: 9780553283686") {
		t.Errorf("isbn not written; got:\n%s", h)
	}
	if !strings.Contains(h, "_Imported from Goodreads on 2026-04-17_") {
		t.Errorf("missing provenance line; got:\n%s", h)
	}
	if !strings.Contains(h, "> Loved the structure.") {
		t.Errorf("review not blockquote-wrapped; got:\n%s", h)
	}
	// Reading Timeline gets an Added-to-shelf entry.
	if !strings.Contains(h, "2024-12-15 — Added to shelf") {
		t.Errorf("timeline entry missing; got:\n%s", h)
	}
	// Finished array populated because shelf was read with Date Read.
	if !strings.Contains(h, "2025-04-02") {
		t.Errorf("finished date missing; got:\n%s", h)
	}
}

func TestGoodreadsImport_HostileCSV_PathTraversalInTitle(t *testing.T) {
	booksAbs, s, syncer := setup(t)
	backupsRoot := filepath.Join(filepath.Dir(booksAbs), "backups")
	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	csvText := `Title,Author,Exclusive Shelf
"../../evil","Attacker","to-read"
`
	records, err := goodreads.NewReader(strings.NewReader(csvText)).ReadAll()
	if err != nil {
		t.Fatalf("CSV: %v", err)
	}
	rv, _ := goodreads.NewResolver(context.Background(), s)
	plan, err := goodreads.BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The title is sanitized (slashes → fraction-slash), so the generated
	// filename is a leaf inside the Books folder — not a traversal.
	if len(plan.WillCreate) != 1 {
		t.Fatalf("expected 1 create; got %+v", plan)
	}
	filename := plan.WillCreate[0].Filename
	if strings.Contains(filename, "..") && !strings.Contains(filename, "..\u2044") {
		t.Errorf("unsanitized filename %q", filename)
	}

	_, err = goodreads.Apply(context.Background(), plan, booksAbs, goodreads.ApplyOptions{
		Syncer: syncer, BackupsRoot: backupsRoot, ImportStamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Confirm the file landed inside booksAbs, not outside.
	entries, err := os.ReadDir(booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	foundInside := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "evil") {
			foundInside = true
		}
	}
	if !foundInside {
		t.Errorf("sanitized file did not land inside books dir; entries: %v", entries)
	}
}

func TestGoodreadsImport_BackupCapturesPreexisting(t *testing.T) {
	booksAbs, s, syncer := setup(t)
	backupsRoot := filepath.Join(filepath.Dir(booksAbs), "backups")
	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	// Pre-existing note that shouldn't be modified (empty CSV → no plan
	// entries that touch it), but backup must still capture it.
	writeFile(t, filepath.Join(booksAbs, "Preexisting by User.md"), hyperion)

	csvText := `Title,Author,Exclusive Shelf
"New Book","Nobody","to-read"
`
	records, _ := goodreads.NewReader(strings.NewReader(csvText)).ReadAll()
	rv, _ := goodreads.NewResolver(context.Background(), s)
	plan, _ := goodreads.BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	report, err := goodreads.Apply(context.Background(), plan, booksAbs, goodreads.ApplyOptions{
		Syncer: syncer, BackupsRoot: backupsRoot, ImportStamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(report.BackupRoot, "Preexisting by User.md")
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup missing preexisting file: %v", err)
	}
}

