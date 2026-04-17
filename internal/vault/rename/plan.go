package rename

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Plan lists the proposed renames, along with skips (already canonical)
// and conflicts (missing frontmatter, target exists, validation fails).
type Plan struct {
	WillRename []RenameEntry   `json:"will_rename"`
	WillSkip   []SkipEntry     `json:"will_skip"`
	Conflicts  []ConflictEntry `json:"conflicts"`
}

// RenameEntry describes a single proposed move from OldFilename to
// NewFilename (both relative to the Books folder).
type RenameEntry struct {
	OldFilename string `json:"old_filename"`
	NewFilename string `json:"new_filename"`
	Reason      string `json:"reason"`
}

// SkipEntry records a non-canonical file that doesn't need moving after
// all (usually because the canonical name equals the current one — rare
// but possible after a previous rename attempt).
type SkipEntry struct {
	OldFilename string `json:"old_filename"`
	Reason      string `json:"reason"`
}

// ConflictEntry is a rename the pipeline refuses to auto-apply.
type ConflictEntry struct {
	OldFilename       string `json:"old_filename"`
	Reason            string `json:"reason"`
	NeedsUserDecision bool   `json:"needs_user_decision"`
}

// BuildPlan scans the store for books with CanonicalName == false and
// proposes a rename for each. No file writes; the returned Plan is
// JSON-serializable for UI display.
func BuildPlan(ctx context.Context, s *store.Store, booksAbs string) (*Plan, error) {
	rows, err := s.ListBooks(ctx, store.Filter{})
	if err != nil {
		return nil, fmt.Errorf("rename: listing books: %w", err)
	}

	p := &Plan{}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row.CanonicalName {
			continue
		}
		if row.Title == "" || len(row.Authors) == 0 {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				OldFilename:       row.Filename,
				Reason:            "missing title or authors[0]; cannot generate canonical filename",
				NeedsUserDecision: true,
			})
			continue
		}
		newName, err := paths.Generate(row.Title, row.Authors[0])
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				OldFilename:       row.Filename,
				Reason:            fmt.Sprintf("paths.Generate: %v", err),
				NeedsUserDecision: true,
			})
			continue
		}
		if newName == row.Filename {
			p.WillSkip = append(p.WillSkip, SkipEntry{
				OldFilename: row.Filename,
				Reason:      "already matches canonical form",
			})
			continue
		}
		// Validate new filename.
		newAbs, err := paths.ValidateWithinRoot(booksAbs, newName)
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				OldFilename:       row.Filename,
				Reason:            fmt.Sprintf("new filename validation: %v", err),
				NeedsUserDecision: true,
			})
			continue
		}
		// Target must not already exist on disk.
		if _, statErr := os.Stat(newAbs); statErr == nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				OldFilename:       row.Filename,
				Reason:            fmt.Sprintf("target %q already exists", newName),
				NeedsUserDecision: true,
			})
			continue
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				OldFilename:       row.Filename,
				Reason:            fmt.Sprintf("stat target: %v", statErr),
				NeedsUserDecision: true,
			})
			continue
		}
		p.WillRename = append(p.WillRename, RenameEntry{
			OldFilename: row.Filename,
			NewFilename: newName,
			Reason:      "frontmatter title + authors[0] maps to canonical form",
		})
	}

	sort.Slice(p.WillRename, func(i, j int) bool { return p.WillRename[i].OldFilename < p.WillRename[j].OldFilename })
	sort.Slice(p.WillSkip, func(i, j int) bool { return p.WillSkip[i].OldFilename < p.WillSkip[j].OldFilename })
	sort.Slice(p.Conflicts, func(i, j int) bool { return p.Conflicts[i].OldFilename < p.Conflicts[j].OldFilename })
	return p, nil
}

// MarshalJSON ensures empty slices render as "[]" rather than "null".
func (p *Plan) MarshalJSON() ([]byte, error) {
	type jsonPlan struct {
		WillRename []RenameEntry   `json:"will_rename"`
		WillSkip   []SkipEntry     `json:"will_skip"`
		Conflicts  []ConflictEntry `json:"conflicts"`
	}
	out := jsonPlan{
		WillRename: p.WillRename,
		WillSkip:   p.WillSkip,
		Conflicts:  p.Conflicts,
	}
	if out.WillRename == nil {
		out.WillRename = []RenameEntry{}
	}
	if out.WillSkip == nil {
		out.WillSkip = []SkipEntry{}
	}
	if out.Conflicts == nil {
		out.Conflicts = []ConflictEntry{}
	}
	return marshalJSON(out)
}
