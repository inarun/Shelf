package goodreads

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Plan captures the outcome of applying the dry-run classifier to a
// []Record against a Resolver. It is safe to serialize as JSON and
// present to the user for confirmation; Apply consumes the exact same
// Plan struct to perform the writes.
type Plan struct {
	WillCreate []CreateEntry   `json:"will_create"`
	WillUpdate []UpdateEntry   `json:"will_update"`
	WillSkip   []SkipEntry     `json:"will_skip"`
	Conflicts  []ConflictEntry `json:"conflicts"`
}

// CreateEntry describes a note that does not yet exist and will be
// created by Apply.
type CreateEntry struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
	Preview  string `json:"preview"`

	// record is consumed by Apply; omitted from JSON via the lowercase
	// name. Not exported because external code has no use for it.
	record Record
}

// UpdateEntry describes a gap-fill against an existing note. Apply
// re-validates the plan's (size, mtime) against the live file to catch
// between-plan-and-apply drift.
type UpdateEntry struct {
	Filename string        `json:"filename"`
	Reason   string        `json:"reason"`
	Changes  []FieldChange `json:"changes"`

	record    Record
	planSize  int64
	planMtime int64
}

// FieldChange describes a single frontmatter (or body.notes) mutation
// the importer proposes. Old is the current vault value; New is the
// incoming CSV value.
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// SkipEntry describes a record where no change is warranted (all fields
// already populated in the vault).
type SkipEntry struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
}

// ConflictEntry describes a record that cannot be auto-classified:
// missing identity, fuzzy ambiguity, reserved filename, etc. The UI
// surfaces these for explicit user resolution.
type ConflictEntry struct {
	Filename          string `json:"filename,omitempty"`
	Reason            string `json:"reason"`
	NeedsUserDecision bool   `json:"needs_user_decision"`
	CSVRow            int    `json:"csv_row"`

	record Record
}

// BuildPlan pairs each Record against rv and classifies the outcome.
// It performs vault reads (to compute per-field diffs on update
// candidates) but no writes. booksAbs must be the resolved Books folder
// absolute path; importStamp is the provenance-line date that Apply
// will embed in the ## Notes section for imported reviews.
func BuildPlan(ctx context.Context, records []Record, rv *Resolver, booksAbs string, importStamp time.Time) (*Plan, error) {
	p := &Plan{}
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// No identifying information → cannot generate a filename or
		// match against the vault.
		if r.Title == "" || len(r.Authors) == 0 {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Reason:            "missing title and/or author — cannot generate filename",
				NeedsUserDecision: true,
				CSVRow:            r.RowNum,
				record:            r,
			})
			continue
		}

		if res, ok := rv.Match(r); ok {
			if res.NeedsUserDecision {
				p.Conflicts = append(p.Conflicts, ConflictEntry{
					Filename:          res.Filename,
					Reason:            res.Reason,
					NeedsUserDecision: true,
					CSVRow:            r.RowNum,
					record:            r,
				})
				continue
			}

			fullPath, err := paths.ValidateWithinRoot(booksAbs, res.Filename)
			if err != nil {
				p.Conflicts = append(p.Conflicts, ConflictEntry{
					Filename:          res.Filename,
					Reason:            fmt.Sprintf("match validation failed: %v", err),
					NeedsUserDecision: true,
					CSVRow:            r.RowNum,
					record:            r,
				})
				continue
			}
			n, err := note.Read(fullPath)
			if err != nil {
				p.Conflicts = append(p.Conflicts, ConflictEntry{
					Filename:          res.Filename,
					Reason:            fmt.Sprintf("reading matched note: %v", err),
					NeedsUserDecision: true,
					CSVRow:            r.RowNum,
					record:            r,
				})
				continue
			}
			changes := computeChanges(r, n)
			if len(changes) == 0 {
				p.WillSkip = append(p.WillSkip, SkipEntry{
					Filename: res.Filename,
					Reason:   "all fields already populated",
				})
				continue
			}
			p.WillUpdate = append(p.WillUpdate, UpdateEntry{
				Filename:  res.Filename,
				Reason:    res.Reason,
				Changes:   changes,
				record:    r,
				planSize:  n.Size,
				planMtime: n.MtimeNanos,
			})
			continue
		}

		// No match — propose a create.
		filename, err := paths.Generate(r.Title, r.Authors[0])
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Reason:            fmt.Sprintf("cannot generate canonical filename: %v", err),
				NeedsUserDecision: true,
				CSVRow:            r.RowNum,
				record:            r,
			})
			continue
		}
		fullPath, err := paths.ValidateWithinRoot(booksAbs, filename)
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          filename,
				Reason:            fmt.Sprintf("filename validation failed: %v", err),
				NeedsUserDecision: true,
				CSVRow:            r.RowNum,
				record:            r,
			})
			continue
		}
		if _, err := os.Stat(fullPath); err == nil {
			// A note with this canonical filename already exists but
			// didn't match — usually an ISBN gap in the vault note.
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          filename,
				Reason:            "target filename already exists but match lookup missed",
				NeedsUserDecision: true,
				CSVRow:            r.RowNum,
				record:            r,
			})
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          filename,
				Reason:            fmt.Sprintf("stat %s: %v", filepath.Base(fullPath), err),
				NeedsUserDecision: true,
				CSVRow:            r.RowNum,
				record:            r,
			})
			continue
		}

		p.WillCreate = append(p.WillCreate, CreateEntry{
			Filename: filename,
			Reason:   "no matching note found",
			Preview:  Preview(r),
			record:   r,
		})
	}

	// Sort for deterministic JSON output.
	sort.Slice(p.WillCreate, func(i, j int) bool { return p.WillCreate[i].Filename < p.WillCreate[j].Filename })
	sort.Slice(p.WillUpdate, func(i, j int) bool { return p.WillUpdate[i].Filename < p.WillUpdate[j].Filename })
	sort.Slice(p.WillSkip, func(i, j int) bool { return p.WillSkip[i].Filename < p.WillSkip[j].Filename })
	sort.Slice(p.Conflicts, func(i, j int) bool {
		if p.Conflicts[i].Filename == p.Conflicts[j].Filename {
			return p.Conflicts[i].CSVRow < p.Conflicts[j].CSVRow
		}
		return p.Conflicts[i].Filename < p.Conflicts[j].Filename
	})

	_ = importStamp // referenced for future diffs that include stamp in reasons
	return p, nil
}

// Preview renders a single-line human summary of r for the dry-run UI.
// No newlines, no absolute paths, no review body text.
func Preview(r Record) string {
	isbn := r.ISBN13
	kind := "ISBN-13"
	if isbn == "" {
		isbn = r.ISBN10
		kind = "ISBN-10"
	}
	if isbn == "" {
		isbn = "—"
		kind = "ISBN"
	}
	shelf := r.ExclusiveShelf
	if shelf == "" {
		shelf = "—"
	}
	author := ""
	if len(r.Authors) > 0 {
		author = r.Authors[0]
	}
	return fmt.Sprintf("%s by %s (%s %s, shelf: %s, rating: %d)",
		r.Title, author, kind, isbn, shelf, r.MyRating)
}
