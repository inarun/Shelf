package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/backup"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// ApplyOptions controls Apply's execution. Mirrors rename.ApplyOptions
// and audiobookshelf.ApplyOptions for symmetry.
type ApplyOptions struct {
	// Syncer is required — Apply drives sync.Apply on every successful
	// write so the index stays coherent without relying on the watcher.
	Syncer *sync.Syncer
	// BackupsRoot is the absolute path to {data.directory}/backups.
	// Required unless SkipBackup is set.
	BackupsRoot string
	// SkipBackup disables the pre-apply snapshot. Tests only.
	// Production must always back up before bulk writing (Core Invariant #7).
	SkipBackup bool
}

// ApplyReport summarizes the outcome of Apply. Per-entry errors land in
// Errors; Apply returns an error only if the backup itself failed.
type ApplyReport struct {
	BackupRoot string       `json:"backup_root"`
	Migrated   []string     `json:"migrated"`
	Skipped    []string     `json:"skipped"`
	Errors     []ApplyError `json:"errors"`
}

// ApplyError is a per-entry failure. Phase is one of "validate",
// "read", "shape", "stale", "serialize", or "reindex".
type ApplyError struct {
	Filename string `json:"filename"`
	Phase    string `json:"phase"`
	Err      error  `json:"-"`
	// Message is the human-readable rendering of Err, used for JSON
	// encoding since Go's errors don't serialize by default.
	Message string `json:"error"`
}

// Error renders ApplyError for humans / error joining.
func (e ApplyError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Phase, e.Filename, e.Err)
}

// Apply executes p. Pre-apply backup (unless SkipBackup), then per
// MigrateEntry: validate path → re-read note → check staleness pair →
// assert shape is still RatingShapeLegacyScalar → call SetRating on the
// same Rating value (normalizes to map shape) → regenerate the body
// `## Rating` block → atomic SaveBody → reindex. WillSkip entries are
// recorded in report.Skipped; Conflicts are untouched by Apply (the
// handler promotes them via ApplyDecisions first if the user accepts).
func Apply(ctx context.Context, p *Plan, booksAbs string, opts ApplyOptions) (*ApplyReport, error) {
	if opts.Syncer == nil {
		return nil, errors.New("migrate: Apply requires a Syncer")
	}
	report := &ApplyReport{}
	if !opts.SkipBackup {
		info, err := backup.Snapshot(ctx, booksAbs, opts.BackupsRoot)
		if err != nil {
			return nil, fmt.Errorf("migrate: pre-apply backup: %w", err)
		}
		report.BackupRoot = info.Root
	}

	for _, entry := range p.WillMigrate {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		applyOne(ctx, entry, booksAbs, opts, report)
	}
	for _, entry := range p.WillSkip {
		report.Skipped = append(report.Skipped, entry.Filename)
	}
	return report, nil
}

func applyOne(ctx context.Context, entry MigrateEntry, booksAbs string, opts ApplyOptions, report *ApplyReport) {
	fullPath, err := paths.ValidateWithinRoot(booksAbs, entry.Filename)
	if err != nil {
		appendErr(report, entry.Filename, "validate", err)
		return
	}
	n, err := note.Read(fullPath)
	if err != nil {
		appendErr(report, entry.Filename, "read", err)
		return
	}
	if n.Size != entry.planSize || n.MtimeNanos != entry.planMtime {
		appendErr(report, entry.Filename, "stale", note.ErrStale)
		return
	}
	// If the shape flipped between Plan and Apply (e.g., the user saved
	// via the widget mid-migration), skip rather than clobber.
	if n.Frontmatter.RatingShape() != frontmatter.RatingShapeLegacyScalar {
		appendErr(report, entry.Filename, "shape", errors.New("rating shape changed since plan"))
		return
	}
	r := n.Frontmatter.Rating()
	if r == nil {
		appendErr(report, entry.Filename, "shape", errors.New("rating vanished since plan"))
		return
	}
	// SetRating with the same semantic Rating re-serializes as the
	// canonical map shape. TrialSystem stays empty (legacy scalars had
	// no per-axis data); Overall is preserved.
	if err := n.Frontmatter.SetRating(r); err != nil {
		appendErr(report, entry.Filename, "serialize", err)
		return
	}
	// Dual-write the body `## Rating` block so viewers that read the
	// body instead of frontmatter get the same info.
	n.Body.SetRatingFromFrontmatter(r)
	if err := n.SaveBody(); err != nil {
		appendErr(report, entry.Filename, "serialize", err)
		return
	}
	if err := opts.Syncer.Apply(ctx, sync.Event{Kind: sync.EventWrite, Path: fullPath}); err != nil {
		appendErr(report, entry.Filename, "reindex", err)
		return
	}
	report.Migrated = append(report.Migrated, entry.Filename)
}

func appendErr(report *ApplyReport, filename, phase string, err error) {
	report.Errors = append(report.Errors, ApplyError{
		Filename: filename,
		Phase:    phase,
		Err:      err,
		Message:  err.Error(),
	})
}
