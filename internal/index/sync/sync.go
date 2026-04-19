package sync

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Syncer walks the books directory and keeps the SQLite index aligned
// with the vault. booksDir must be an absolute path to the configured
// books folder; every call revalidates file paths against it through
// internal/vault/paths.ValidateWithinVault.
type Syncer struct {
	store    *store.Store
	booksDir string
}

// New constructs a Syncer. booksDir is stored as-is; callers should
// pass the already-resolved absolute path from config.
func New(s *store.Store, booksDir string) *Syncer {
	return &Syncer{store: s, booksDir: booksDir}
}

// FullScan walks the books directory and reconciles every .md file
// against the index. Files whose (size, mtime_ns) matches the index
// row are skipped without re-parsing. Files not in the vault are
// deleted from the index. Per-file errors accumulate into
// Report.Errors without aborting the scan.
func (sy *Syncer) FullScan(ctx context.Context) (Report, error) {
	var rep Report

	onDiskStats, onDiskErr := sy.walk(&rep)
	if onDiskErr != nil {
		return rep, onDiskErr
	}

	indexed, err := sy.store.AllFilenames(ctx)
	if err != nil {
		return rep, err
	}

	for filename, onDisk := range onDiskStats {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		existing, ok := indexed[filename]
		if ok && existing.SizeBytes == onDisk.SizeBytes && existing.MtimeNanos == onDisk.MtimeNanos {
			rep.Skipped++
			continue
		}
		if err := sy.readAndIndex(ctx, filename); err != nil {
			rep.Errors = append(rep.Errors, FileError{Filename: filename, Err: err})
			continue
		}
		rep.Indexed++
	}

	for filename := range indexed {
		if _, stillOnDisk := onDiskStats[filename]; stillOnDisk {
			continue
		}
		if err := sy.store.DeleteBookByFilename(ctx, filename); err != nil && !errors.Is(err, store.ErrNotFound) {
			rep.Errors = append(rep.Errors, FileError{Filename: filename, Err: err})
			continue
		}
		rep.Deleted++
	}

	return rep, nil
}

// Apply dispatches a single watcher event to the appropriate index
// operation. Create/Write paths re-parse the file; Remove/Rename paths
// drop the affected row(s).
func (sy *Syncer) Apply(ctx context.Context, ev Event) error {
	base := filepath.Base(ev.Path)
	if !isIndexable(base) {
		return nil
	}
	switch ev.Kind {
	case EventCreate, EventWrite:
		return sy.readAndIndex(ctx, base)
	case EventRemove:
		if err := sy.store.DeleteBookByFilename(ctx, base); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return nil
	case EventRename:
		if ev.OldPath != "" {
			oldBase := filepath.Base(ev.OldPath)
			if err := sy.store.DeleteBookByFilename(ctx, oldBase); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return sy.readAndIndex(ctx, base)
	}
	return nil
}

// walk visits every entry in booksDir (non-recursive per spec; book
// notes live flat in the configured folder) and captures .md files
// with their on-disk staleness pair. Subdirectories are skipped for
// v0.1; a future enhancement could recurse if the user adopts a
// folder-per-series layout.
func (sy *Syncer) walk(rep *Report) (map[string]store.FileStat, error) {
	out := map[string]store.FileStat{}
	err := filepath.WalkDir(sy.booksDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == sy.booksDir {
				return nil
			}
			// Skip subdirectories for v0.1.
			return filepath.SkipDir
		}
		base := d.Name()
		if !isIndexable(base) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			rep.Errors = append(rep.Errors, FileError{Filename: base, Err: err})
			return nil
		}
		rep.Scanned++
		out[base] = store.FileStat{
			SizeBytes:  info.Size(),
			MtimeNanos: info.ModTime().UnixNano(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sync: walk %s: %w", sy.booksDir, err)
	}
	return out, nil
}

// tempFileRe matches the exact shape emitted by internal/vault/atomic.
// 16 lowercase hex chars between the basename and the .shelf.tmp
// suffix, anchored at both ends. Validated further via hex.DecodeString
// to make sure coincidental-looking user files aren't ignored.
var tempFileRe = regexp.MustCompile(`^\.(.+)\.([0-9a-f]{16})\.shelf\.tmp$`)

// isIndexable reports whether base is a candidate for the index: .md
// suffix, not a Shelf temp file, not a dotfile.
func isIndexable(base string) bool {
	if base == "" || strings.HasPrefix(base, ".") {
		return false
	}
	if !strings.EqualFold(filepath.Ext(base), ".md") {
		return false
	}
	if isShelfTempFile(base) {
		return false
	}
	return true
}

// isShelfTempFile reports whether base is a temp file produced by the
// atomic writer. The hex group is verified via hex.DecodeString so a
// user file that happens to match the regex but isn't a real hex token
// is still indexed.
func isShelfTempFile(base string) bool {
	m := tempFileRe.FindStringSubmatch(base)
	if m == nil {
		return false
	}
	if _, err := hex.DecodeString(m[2]); err != nil {
		return false
	}
	return true
}

func (sy *Syncer) readAndIndex(ctx context.Context, filename string) error {
	fullPath, err := paths.ValidateWithinVault(sy.booksDir, filepath.Join(sy.booksDir, filename))
	if err != nil {
		return fmt.Errorf("sync: path validate %s: %w", filename, err)
	}
	n, err := note.Read(fullPath)
	if err != nil {
		return err
	}
	row := buildBookRow(filename, n)
	if _, err := sy.store.UpsertBook(ctx, row); err != nil {
		return err
	}
	return nil
}

// buildBookRow turns a parsed Note into the flat BookRow the store
// wants. Field mapping follows SKILL.md §Frontmatter schema. Warnings
// aggregate any non-fatal issues encountered during extraction —
// non-canonical filenames, missing critical fields, inconsistent date
// arrays — so the UI can surface them alongside the indexed row.
func buildBookRow(filename string, n *note.Note) store.BookRow {
	fm := n.Frontmatter

	title := fm.Title()
	var warnings []string

	canonical := true
	parsedTitle, parsedAuthor, err := paths.Parse(filename)
	if err != nil {
		canonical = false
		warnings = append(warnings,
			fmt.Sprintf("filename %q does not match canonical {Title} by {Author}.md", filename))
	} else if title == "" {
		// No frontmatter title — fall back to the filename-derived title.
		title = parsedTitle
	}

	authors := fm.Authors()
	if len(authors) == 0 && canonical && parsedAuthor != "" {
		authors = []string{parsedAuthor}
	}
	if title == "" {
		warnings = append(warnings, "missing title in frontmatter and filename")
	}

	startedDates := formatDateSlice(fm.Started())
	finishedDates := formatDateSlice(fm.Finished())
	if len(startedDates) < len(finishedDates) {
		warnings = append(warnings, "finished has more entries than started")
	}

	status := fm.Status()
	if status == "" {
		status = "unread"
	}

	readCount := int64(fm.ReadCount())

	// v0.2.1 Session 16: fan the Rating struct out into three columns.
	// RatingOverall carries the full float (possibly >5), RatingDimensions
	// serialises the per-axis map (empty when not dimensioned), and
	// RatingHasOverride distinguishes explicit from derived overalls so
	// the recommender (v0.3) can weight them differently.
	var (
		ratingOverall     *float64
		ratingDimensions  map[string]int
		ratingHasOverride bool
	)
	if r := fm.Rating(); r != nil && !r.IsEmpty() {
		v := r.Effective()
		ratingOverall = &v
		ratingDimensions = r.TrialSystem
		ratingHasOverride = r.HasOverride()
	}

	var totalPages *int64
	if p := fm.TotalPages(); p != nil {
		pp := int64(*p)
		totalPages = &pp
	}

	seriesName := fm.Series()
	seriesIndex := fm.SeriesIndex()

	row := store.BookRow{
		Filename:          filename,
		CanonicalName:     canonical,
		Title:             title,
		Subtitle:          fm.Subtitle(),
		Publisher:         fm.Publisher(),
		PublishDate:       fm.Publish(),
		TotalPages:        totalPages,
		ISBN:              fm.ISBN(),
		Cover:             fm.Cover(),
		Format:            fm.Format(),
		Source:            fm.Source(),
		RatingOverall:     ratingOverall,
		RatingDimensions:  ratingDimensions,
		RatingHasOverride: ratingHasOverride,
		Status:            status,
		ReadCount:         readCount,
		SeriesName:        seriesName,
		SeriesIndex:       seriesIndex,
		Authors:           authors,
		Categories:        fm.Categories(),
		StartedDates:      startedDates,
		FinishedDates:     finishedDates,
		SizeBytes:         n.Size,
		MtimeNanos:        n.MtimeNanos,
		IndexedAtUnix:     time.Now().Unix(),
		Warnings:          warnings,
	}
	return row
}

func formatDateSlice(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Format("2006-01-02"))
	}
	return out
}
