package audiobookshelf

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/inarun/Shelf/internal/domain/precedence"
	"github.com/inarun/Shelf/internal/domain/timeline"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/backup"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// ApplyOptions controls Apply's execution.
type ApplyOptions struct {
	// Syncer is required: Apply drives sync.Apply on every write so the
	// index stays coherent without relying on the watcher.
	Syncer *sync.Syncer
	// BackupsRoot is the absolute path to {data.directory}/backups.
	// Required unless SkipBackup is set.
	BackupsRoot string
	// SkipBackup disables the pre-apply snapshot. Tests only. Production
	// must always back up before bulk writing (Core Invariant #7).
	SkipBackup bool
}

// ApplyReport summarizes the outcome of Apply. Per-entry errors land in
// Errors; Apply returns an error only if the backup itself failed.
type ApplyReport struct {
	BackupRoot string
	Updated    []string
	Skipped    []string
	Errors     []ApplyError
}

// ApplyError is a per-entry failure. Phase is one of "backup",
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
//  2. For each WillUpdate: re-read note, enforce staleness pair, merge
//     new timeline entries, serialize body + frontmatter, atomic write,
//     reindex.
//  3. Record WillSkip entries; no writes.
//
// Unmatched and Conflicts buckets are not touched by Apply — the
// handler is expected to promote conflicts via ApplyDecisions first
// (symmetric with Goodreads); unmatched items are reported back to the
// UI for S14 to surface.
func Apply(ctx context.Context, p *Plan, booksAbs string, opts ApplyOptions) (*ApplyReport, error) {
	if opts.Syncer == nil {
		return nil, errors.New("audiobookshelf: Apply requires a Syncer")
	}

	report := &ApplyReport{}
	if !opts.SkipBackup {
		info, err := backup.Snapshot(ctx, booksAbs, opts.BackupsRoot)
		if err != nil {
			return nil, fmt.Errorf("audiobookshelf: pre-apply backup: %w", err)
		}
		report.BackupRoot = info.Root
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

// applyUpdate performs one entry's rewrite: staleness check → mutate
// frontmatter + body → SaveBody → reindex.
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
	// Between-plan-and-apply drift check.
	if n.Size != entry.planSize || n.MtimeNanos != entry.planMtime {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "update", Err: note.ErrStale,
		})
		return
	}

	if err := applyEntriesToNote(n, entry.NewEntries); err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "update", Err: err,
		})
		return
	}

	if err := n.SaveBody(); err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "update", Err: err,
		})
		return
	}
	if err := opts.Syncer.Apply(ctx, sync.Event{Kind: sync.EventWrite, Path: fullPath}); err != nil {
		report.Errors = append(report.Errors, ApplyError{
			Filename: entry.Filename, Phase: "reindex", Err: err,
		})
		return
	}
	report.Updated = append(report.Updated, entry.Filename)
}

// applyEntriesToNote translates each new timeline.Entry into
// frontmatter + body mutations. Frontmatter dates fill gaps via
// Append*; status transitions use SourceAudiobookshelf as a
// §Data-precedence candidate so explicit user values win.
//
// The body timeline gets one "## Reading Timeline" line per entry,
// labeled by Kind — "Started listening (Audiobookshelf)" for progress,
// "Finished listening (Audiobookshelf)" for finished.
func applyEntriesToNote(n *note.Note, entries []timeline.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	fm := n.Frontmatter
	for _, e := range entries {
		if e.Source != precedence.SourceAudiobookshelf {
			// Apply only ever writes external-origin entries (vault ones
			// already exist). If a caller passes a vault entry here it's
			// a programming error — guard defensively.
			continue
		}
		switch e.Kind {
		case timeline.KindFinished:
			if !e.End.IsZero() {
				fm.AppendFinished(e.End)
			}
			if !e.Start.IsZero() && !hasStartedDate(fm.Started(), e.Start) {
				fm.AppendStarted(e.Start)
			}
			if statusIsGap(fm.Status()) {
				if err := fm.SetStatus("finished"); err != nil {
					return fmt.Errorf("set status: %w", err)
				}
			}
		case timeline.KindProgress, timeline.KindStarted:
			if !e.Start.IsZero() && !hasStartedDate(fm.Started(), e.Start) {
				fm.AppendStarted(e.Start)
			}
			if statusIsGap(fm.Status()) {
				if err := fm.SetStatus("reading"); err != nil {
					return fmt.Errorf("set status: %w", err)
				}
			}
		}
		// Append a body "## Reading Timeline" line. The body parser is
		// tolerant of empty sections — AppendTimelineEvent creates the
		// section on first use.
		text := e.Note
		if text == "" {
			switch e.Kind {
			case timeline.KindFinished:
				text = "Finished listening (Audiobookshelf)"
			case timeline.KindStarted, timeline.KindProgress:
				text = "Started listening (Audiobookshelf)"
			default:
				text = "Audiobookshelf activity"
			}
		}
		when := e.Start
		if e.Kind == timeline.KindFinished && !e.End.IsZero() {
			when = e.End
		}
		n.Body.AppendTimelineEvent(when, text)
	}
	return nil
}

// hasStartedDate reports whether dates already contains a start
// matching target (date-level equality, UTC). Used to avoid duplicate
// frontmatter entries when a sync re-runs.
func hasStartedDate(dates []time.Time, target time.Time) bool {
	ty, tm, td := target.UTC().Date()
	for _, d := range dates {
		dy, dm, dd := d.UTC().Date()
		if dy == ty && dm == tm && dd == td {
			return true
		}
	}
	return false
}

// statusIsGap reports whether a status value should be overwritten by
// an external source — "" (absent) and "unread" (template default)
// count as gaps per §Data precedence (status exception).
func statusIsGap(s string) bool {
	return s == "" || s == "unread"
}
