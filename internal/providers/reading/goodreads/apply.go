package goodreads

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/backup"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// ApplyOptions controls Apply's execution.
type ApplyOptions struct {
	// Syncer is required: Apply drives sync.Apply on every write so the
	// index stays coherent without relying on the watcher.
	Syncer *sync.Syncer
	// ImportStamp is the provenance-line timestamp embedded in imported
	// reviews. Callers typically pass time.Now(); tests pass a fixed
	// value.
	ImportStamp time.Time
	// SkipBackup disables the pre-apply snapshot. Tests only. Production
	// must always back up before bulk writing per SKILL.md §Core
	// invariant #7.
	SkipBackup bool
	// BackupsRoot is the absolute path to {data.directory}/backups.
	// Required unless SkipBackup is set.
	BackupsRoot string
}

// ApplyReport summarizes the outcome of Apply. Per-entry errors land in
// Errors; Apply returns an error only if the backup itself failed.
type ApplyReport struct {
	BackupRoot string
	Created    []string
	Updated    []string
	Skipped    []string
	Errors     []ApplyError
}

// ApplyError is a per-entry failure. Phase is one of "backup", "create",
// "update", or "reindex" — surfacing which step broke for which file.
type ApplyError struct {
	Filename string
	Phase    string
	Err      error
}

// Error renders ApplyError for humans / error joining.
func (e ApplyError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Phase, e.Filename, e.Err)
}

// Apply executes p against the vault. booksAbs must be the resolved
// Books folder path. Flow:
//
//  1. Snapshot the Books folder (skipped only in tests).
//  2. Write every WillCreate note via note.Create + reindex.
//  3. Apply every WillUpdate to the existing note via SaveBody (for
//     body changes) or SaveFrontmatter (frontmatter-only) + reindex.
//  4. Record WillSkip entries; no writes.
//
// Per-entry errors accumulate in ApplyReport.Errors. Apply returns a
// non-nil error only when the backup step itself fails; in that case no
// writes have happened and the returned *ApplyReport is nil.
func Apply(ctx context.Context, p *Plan, booksAbs string, opts ApplyOptions) (*ApplyReport, error) {
	if opts.Syncer == nil {
		return nil, errors.New("goodreads: Apply requires a Syncer")
	}

	report := &ApplyReport{}
	if !opts.SkipBackup {
		info, err := backup.Snapshot(ctx, booksAbs, opts.BackupsRoot)
		if err != nil {
			return nil, fmt.Errorf("goodreads: pre-apply backup: %w", err)
		}
		report.BackupRoot = info.Root
	}

	for _, entry := range p.WillCreate {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		applyCreate(ctx, entry, booksAbs, opts, report)
	}
	for _, entry := range p.WillUpdate {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		applyUpdate(ctx, entry, booksAbs, opts, report)
	}
	for _, entry := range p.WillSkip {
		report.Skipped = append(report.Skipped, entry.Filename)
	}
	return report, nil
}

func applyCreate(ctx context.Context, entry CreateEntry, booksAbs string, opts ApplyOptions, report *ApplyReport) {
	fullPath, err := paths.ValidateWithinRoot(booksAbs, entry.Filename)
	if err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "create", Err: err,
		})
		return
	}
	if _, err := os.Stat(fullPath); err == nil {
		// Appeared between plan and apply — don't clobber.
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "create", Err: fs.ErrExist,
		})
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "create", Err: err,
		})
		return
	}

	fm, bd, err := buildNewNote(entry.record, opts.ImportStamp)
	if err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "create", Err: err,
		})
		return
	}
	if err := note.Create(fullPath, fm, bd); err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "create", Err: err,
		})
		return
	}
	if err := opts.Syncer.Apply(ctx, sync.Event{Kind: sync.EventCreate, Path: fullPath}); err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "reindex", Err: err,
		})
		return
	}
	report.Created = append(report.Created, entry.Filename)
}

func applyUpdate(ctx context.Context, entry UpdateEntry, booksAbs string, opts ApplyOptions, report *ApplyReport) {
	fullPath, err := paths.ValidateWithinRoot(booksAbs, entry.Filename)
	if err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "update", Err: err,
		})
		return
	}
	n, err := note.Read(fullPath)
	if err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "update", Err: err,
		})
		return
	}
	// Between-plan-and-apply drift: if the file changed since BuildPlan
	// stamped it, surface a staleness error and let the user re-run.
	if n.Size != entry.planSize || n.MtimeNanos != entry.planMtime {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "update", Err: note.ErrStale,
		})
		return
	}

	hasReview := false
	for _, c := range entry.Changes {
		if err := applyChange(n, c, opts.ImportStamp); err != nil {
			report.Errors = append(report.Errors, ApplyError{
				Filename: entry.Filename, Phase: "update", Err: err,
			})
			return
		}
		if c.Field == "body.notes" {
			hasReview = true
		}
	}

	if hasReview {
		if err := n.SaveBody(); err != nil {
			report.Errors = append(report.Errors, ApplyError{
				Filename: entry.Filename, Phase: "update", Err: err,
			})
			return
		}
	} else {
		if err := n.SaveFrontmatter(); err != nil {
			report.Errors = append(report.Errors, ApplyError{
				Filename: entry.Filename, Phase: "update", Err: err,
			})
			return
		}
	}
	if err := opts.Syncer.Apply(ctx, sync.Event{Kind: sync.EventWrite, Path: fullPath}); err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "reindex", Err: err,
		})
		return
	}
	report.Updated = append(report.Updated, entry.Filename)
}

// applyChange mutates n in place to effect a single FieldChange. The
// Field name is either a frontmatter key or the sentinel "body.notes".
func applyChange(n *note.Note, c FieldChange, importStamp time.Time) error {
	fm := n.Frontmatter
	switch c.Field {
	case frontmatter.KeyAuthors:
		if v, ok := c.New.([]string); ok {
			fm.SetAuthors(v)
		}
	case frontmatter.KeyCategories:
		if v, ok := c.New.([]string); ok {
			fm.SetCategories(v)
		}
	case frontmatter.KeyISBN:
		if v, ok := c.New.(string); ok {
			fm.SetISBN(v)
		}
	case frontmatter.KeyPublisher:
		if v, ok := c.New.(string); ok {
			fm.SetPublisher(v)
		}
	case frontmatter.KeyPublish:
		if v, ok := c.New.(string); ok {
			fm.SetPublish(v)
		}
	case frontmatter.KeyTotalPages:
		if v, ok := c.New.(int); ok {
			return fm.SetTotalPages(&v)
		}
	case frontmatter.KeySubtitle:
		if v, ok := c.New.(string); ok {
			fm.SetSubtitle(v)
		}
	case frontmatter.KeySeries:
		if v, ok := c.New.(string); ok {
			fm.SetSeries(v)
		}
	case frontmatter.KeySeriesIndex:
		if v, ok := c.New.(float64); ok {
			return fm.SetSeriesIndex(&v)
		}
	case frontmatter.KeyRating:
		if v, ok := c.New.(*frontmatter.Rating); ok {
			return fm.SetRating(v)
		}
	case frontmatter.KeyStatus:
		if v, ok := c.New.(string); ok {
			return fm.SetStatus(v)
		}
	case frontmatter.KeyFinished:
		if v, ok := c.New.([]string); ok {
			for _, s := range v {
				tm, err := time.Parse("2006-01-02", s)
				if err != nil {
					return fmt.Errorf("finished[]: %w", err)
				}
				fm.AppendFinished(tm)
			}
		}
	case frontmatter.KeyReadCount:
		if v, ok := c.New.(int); ok {
			fm.SetReadCount(v)
		}
	case "body.notes":
		if v, ok := c.New.(string); ok {
			n.Body.AppendNotes(composeReviewBody(v, importStamp))
		}
	default:
		return fmt.Errorf("apply: unknown field %q", c.Field)
	}
	return nil
}
