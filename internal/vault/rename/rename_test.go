package rename

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

func setupRenameEnv(t *testing.T) (booksAbs, backupsRoot string, s *store.Store, syncer *sync.Syncer) {
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
	sp := filepath.Join(dir, "index.db")
	st, err := store.Open(sp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return booksAbs, backupsRoot, st, sync.New(st, booksAbs)
}

func seedFile(t *testing.T, booksAbs, filename, title, author string) {
	t.Helper()
	fm := frontmatter.NewEmpty()
	fm.SetTitle(title)
	fm.SetAuthors([]string{author})
	bd := &body.Body{}
	bd.SetTitle(title)
	data, err := fm.Serialize(bd.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if err := atomic.Write(filepath.Join(booksAbs, filename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPlan_ProposesRenameForNonCanonical(t *testing.T) {
	booksAbs, _, s, syncer := setupRenameEnv(t)
	seedFile(t, booksAbs, "My Book - John Doe.md", "My Book", "John Doe")
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillRename) != 1 {
		t.Fatalf("expected 1 rename; got %+v", p)
	}
	if p.WillRename[0].OldFilename != "My Book - John Doe.md" {
		t.Errorf("old filename got %q", p.WillRename[0].OldFilename)
	}
	if p.WillRename[0].NewFilename != "My Book by John Doe.md" {
		t.Errorf("new filename got %q", p.WillRename[0].NewFilename)
	}
}

func TestPlan_SkipsAlreadyCanonical(t *testing.T) {
	booksAbs, _, s, syncer := setupRenameEnv(t)
	seedFile(t, booksAbs, "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons")
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := BuildPlan(context.Background(), s, booksAbs)
	if len(p.WillRename) != 0 {
		t.Errorf("canonical file shouldn't rename; got %+v", p)
	}
}

func TestPlan_ConflictOnTargetExists(t *testing.T) {
	booksAbs, _, s, syncer := setupRenameEnv(t)
	seedFile(t, booksAbs, "My Book - John Doe.md", "My Book", "John Doe")
	seedFile(t, booksAbs, "My Book by John Doe.md", "My Book", "John Doe")
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := BuildPlan(context.Background(), s, booksAbs)
	if len(p.Conflicts) == 0 {
		t.Fatalf("expected conflict when target exists; got %+v", p)
	}
	if !strings.Contains(p.Conflicts[0].Reason, "already exists") {
		t.Errorf("reason got %q", p.Conflicts[0].Reason)
	}
}

func TestApply_HappyPathRenamesAndReindexes(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupRenameEnv(t)
	seedFile(t, booksAbs, "My Book - John Doe.md", "My Book", "John Doe")
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := BuildPlan(context.Background(), s, booksAbs)

	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer: syncer, BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Renamed) != 1 {
		t.Fatalf("expected 1 renamed; got %+v", report)
	}
	if _, err := os.Stat(filepath.Join(booksAbs, "My Book by John Doe.md")); err != nil {
		t.Errorf("new file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(booksAbs, "My Book - John Doe.md")); err == nil {
		t.Errorf("old file still present")
	}
	// Index row should reflect new filename.
	if _, err := s.GetBookByFilename(context.Background(), "My Book by John Doe.md"); err != nil {
		t.Errorf("index lookup new: %v", err)
	}
	if _, err := s.GetBookByFilename(context.Background(), "My Book - John Doe.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("old row should be gone, got %v", err)
	}
	// Backup contains the old file.
	if _, err := os.Stat(filepath.Join(report.BackupRoot, "My Book - John Doe.md")); err != nil {
		t.Errorf("backup missing old file: %v", err)
	}
}

func TestApply_BackupFailureAborts(t *testing.T) {
	booksAbs, _, _, syncer := setupRenameEnv(t)
	_, err := Apply(context.Background(), &Plan{
		WillRename: []RenameEntry{{OldFilename: "a.md", NewFilename: "b.md"}},
	}, booksAbs, ApplyOptions{
		Syncer:      syncer,
		BackupsRoot: "/definitely/not/a/real/directory",
	})
	if err == nil {
		t.Fatal("expected backup error")
	}
}

func TestPlanJSON_EmptySlicesRenderAsArrays(t *testing.T) {
	p := &Plan{}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"will_rename":[]`) {
		t.Errorf("expected [], got %s", s)
	}
	if !strings.Contains(s, `"will_skip":[]`) {
		t.Errorf("expected [], got %s", s)
	}
	if !strings.Contains(s, `"conflicts":[]`) {
		t.Errorf("expected [], got %s", s)
	}
}
