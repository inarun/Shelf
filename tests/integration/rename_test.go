package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/vault/rename"
)

func TestRename_EndToEnd_Pipeline(t *testing.T) {
	booksAbs, s, syncer := setup(t)
	backupsRoot := filepath.Join(filepath.Dir(booksAbs), "backups")
	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	// Seed a non-canonical file and FullScan so canonical_name is
	// flipped off in the index.
	writeFile(t, filepath.Join(booksAbs, "My Book - John Doe.md"), `---
title: My Book
authors:
  - John Doe
status: unread
---
# My Book
`)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	plan, err := rename.BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.WillRename) != 1 {
		t.Fatalf("expected 1 rename; got %+v", plan)
	}

	report, err := rename.Apply(context.Background(), plan, booksAbs, rename.ApplyOptions{
		Syncer:      syncer,
		BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Renamed) != 1 {
		t.Fatalf("expected 1 renamed; got %+v", report)
	}

	// New file exists, old file is gone, index reflects the rename.
	if _, err := os.Stat(filepath.Join(booksAbs, "My Book by John Doe.md")); err != nil {
		t.Errorf("new file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(booksAbs, "My Book - John Doe.md")); err == nil {
		t.Errorf("old file still present")
	}
	if _, err := s.GetBookByFilename(context.Background(), "My Book by John Doe.md"); err != nil {
		t.Errorf("new index row missing: %v", err)
	}
	if _, err := s.GetBookByFilename(context.Background(), "My Book - John Doe.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected old row removed, got %v", err)
	}
	// Backup contains the old filename.
	if _, err := os.Stat(filepath.Join(report.BackupRoot, "My Book - John Doe.md")); err != nil {
		t.Errorf("backup missing old file: %v", err)
	}
}
