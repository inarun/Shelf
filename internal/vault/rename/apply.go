package rename

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/backup"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// ApplyOptions controls Apply's execution.
type ApplyOptions struct {
	Syncer      *sync.Syncer
	BackupsRoot string
	SkipBackup  bool
}

// ApplyReport captures the outcome of Apply.
type ApplyReport struct {
	BackupRoot string
	Renamed    []RenameRecord
	Errors     []ApplyError
}

// RenameRecord is a successful old→new pair recorded by Apply.
type RenameRecord struct {
	Old string
	New string
}

// ApplyError describes a per-entry failure. Phase ∈ {"backup",
// "validate", "stat", "rename", "reindex"}.
type ApplyError struct {
	OldFilename string
	Phase       string
	Err         error
}

// Error renders ApplyError for humans.
func (e ApplyError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Phase, e.OldFilename, e.Err)
}

// Apply executes p. If SkipBackup is false, the Books folder is
// snapshotted first. For each WillRename entry, Apply re-validates both
// paths, confirms the destination still doesn't exist, performs an
// atomic.Rename, and drives the index via sync.EventRename. Per-entry
// errors accumulate in the report; a failed backup aborts the run.
func Apply(ctx context.Context, p *Plan, booksAbs string, opts ApplyOptions) (*ApplyReport, error) {
	if opts.Syncer == nil {
		return nil, errors.New("rename: Apply requires a Syncer")
	}

	report := &ApplyReport{}
	if !opts.SkipBackup {
		info, err := backup.Snapshot(ctx, booksAbs, opts.BackupsRoot)
		if err != nil {
			return nil, fmt.Errorf("rename: pre-apply backup: %w", err)
		}
		report.BackupRoot = info.Root
	}

	for _, entry := range p.WillRename {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		oldAbs, err := paths.ValidateWithinRoot(booksAbs, entry.OldFilename)
		if err != nil {
			report.Errors = append(report.Errors, ApplyError{
				OldFilename: entry.OldFilename, Phase: "validate", Err: err,
			})
			continue
		}
		newAbs, err := paths.ValidateWithinRoot(booksAbs, entry.NewFilename)
		if err != nil {
			report.Errors = append(report.Errors, ApplyError{
				OldFilename: entry.OldFilename, Phase: "validate", Err: err,
			})
			continue
		}
		// Target must still be absent.
		if _, err := os.Stat(newAbs); err == nil {
			report.Errors = append(report.Errors, ApplyError{
				OldFilename: entry.OldFilename, Phase: "stat", Err: fs.ErrExist,
			})
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			report.Errors = append(report.Errors, ApplyError{
				OldFilename: entry.OldFilename, Phase: "stat", Err: err,
			})
			continue
		}
		if err := atomic.Rename(oldAbs, newAbs); err != nil {
			report.Errors = append(report.Errors, ApplyError{
				OldFilename: entry.OldFilename, Phase: "rename", Err: err,
			})
			continue
		}
		if err := opts.Syncer.Apply(ctx, sync.Event{
			Kind: sync.EventRename, Path: newAbs, OldPath: oldAbs,
		}); err != nil {
			report.Errors = append(report.Errors, ApplyError{
				OldFilename: entry.OldFilename, Phase: "reindex", Err: err,
			})
			continue
		}
		report.Renamed = append(report.Renamed, RenameRecord{Old: entry.OldFilename, New: entry.NewFilename})
	}
	return report, nil
}

// marshalJSON is an internal wrapper that bridges encoding/json for the
// MarshalJSON method on Plan; kept in this file to avoid an import
// cycle in plan.go's signatures.
func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }
