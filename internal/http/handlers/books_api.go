package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/inarun/Shelf/internal/http/middleware"
	syncpkg "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
)

// MaxJSONBodyBytes caps PATCH request bodies. Generous headroom over
// MaxReviewBytes (64 KiB) for JSON framing and the other small fields.
const MaxJSONBodyBytes = 128 * 1024

// PatchBook handles PATCH /api/books/{filename}. Request body is a JSON
// object; each field is optional and only applied when present:
//
//   { "rating": 1..5 | null, "status": "<enum>", "review": "<string>" }
//
// Semantics:
//   - rating: null clears; 1..5 sets.
//   - status: must be a valid enum; status transitions apply timeline
//     + started/finished array + read_count side effects. Transitions
//     to "unread" from any non-unread state are rejected (400).
//   - review: any UTF-8 string up to MaxReviewBytes; replaces the full
//     contents of the "## Notes" section verbatim.
//
// Save path: SaveFrontmatter for pure-rating requests (never ErrStale),
// SaveBody for anything that touches the body or status (ErrStale → 409).
// After a successful write the index is updated via syncer.Apply.
func (d *Dependencies) PatchBook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)

	raw := r.PathValue("filename")
	abs, base, err := DecodeAndValidateFilename(d.BooksAbs, raw)
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "filename: "+err.Error())
		return
	}

	patch, err := parsePatchBody(r.Body)
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
		return
	}

	// Input validation at the boundary — before touching disk.
	if patch.ratingSet {
		if err := ValidateRating(patch.rating); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
			return
		}
	}
	if patch.statusSet {
		if err := ValidateStatus(patch.status); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
			return
		}
	}
	if patch.reviewSet {
		if err := ValidateReview(patch.review); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
			return
		}
	}

	// Load the note from disk (staleness pair stamped here).
	n, err := note.Read(abs)
	if err != nil {
		d.Logger.Error("note read",
			"filename", base,
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusNotFound, "not_found", "could not read note")
		return
	}

	currentStatus := n.Frontmatter.Status()
	bodyChanged := false

	// Apply rating mutation.
	if patch.ratingSet {
		if err := n.Frontmatter.SetRating(patch.rating); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "rating: "+err.Error())
			return
		}
	}

	// Apply status mutation + transition side effects.
	if patch.statusSet {
		if err := ValidateStatusTransition(currentStatus, patch.status); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
			return
		}
		if currentStatus != patch.status {
			today := time.Now().UTC().Truncate(24 * time.Hour)
			applyStatusSideEffects(n.Frontmatter, n.Body, currentStatus, patch.status, today)
			bodyChanged = true
		}
		if err := n.Frontmatter.SetStatus(patch.status); err != nil {
			d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "status: "+err.Error())
			return
		}
	}

	// Apply review mutation (overwrites ## Notes verbatim).
	if patch.reviewSet {
		n.Body.SetNotes(patch.review)
		bodyChanged = true
	}

	// Write: pick the lightest save path that preserves correctness.
	if bodyChanged {
		if err := n.SaveBody(); err != nil {
			if errors.Is(err, note.ErrStale) {
				d.writeJSONError(w, r, http.StatusConflict, "stale",
					"This note changed outside Shelf. Reload and try again.")
				return
			}
			d.Logger.Error("save body",
				"filename", base,
				"request_id", middleware.RequestIDFrom(r.Context()),
				"err", err,
			)
			d.writeJSONError(w, r, http.StatusInternalServerError, "server", "write failed")
			return
		}
	} else if patch.ratingSet {
		if err := n.SaveFrontmatter(); err != nil {
			d.Logger.Error("save frontmatter",
				"filename", base,
				"request_id", middleware.RequestIDFrom(r.Context()),
				"err", err,
			)
			d.writeJSONError(w, r, http.StatusInternalServerError, "server", "write failed")
			return
		}
	}
	// If no field was set at all the request is a no-op — we still return
	// the current state to keep the client in sync.

	// Re-index the write.
	if err := d.Syncer.Apply(r.Context(), syncpkg.Event{Kind: syncpkg.EventWrite, Path: abs}); err != nil {
		d.Logger.Warn("sync apply after PATCH",
			"filename", base,
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
	}

	// Audit log — fields-changed only, never values (SKILL.md no-review-text rule).
	d.Logger.Info("patch book",
		"filename", base,
		"request_id", middleware.RequestIDFrom(r.Context()),
		"fields_changed", patch.changedFields(),
	)

	// Echo back the fresh index row + review + timeline.
	updated, err := d.Store.GetBookByFilename(r.Context(), base)
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "post-write read failed")
		return
	}
	view, err := d.buildBookDetailView(r.Context(), updated)
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "post-write view failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":   true,
		"book": patchBookJSON(view),
	})
}

// patchReq tracks which fields were present in the JSON so we can
// distinguish "clear rating" (null) from "don't touch rating" (absent).
type patchReq struct {
	ratingSet bool
	rating    *int
	statusSet bool
	status    string
	reviewSet bool
	review    string
}

func (p patchReq) changedFields() []string {
	out := []string{}
	if p.ratingSet {
		out = append(out, "rating")
	}
	if p.statusSet {
		out = append(out, "status")
	}
	if p.reviewSet {
		out = append(out, "review")
	}
	return out
}

// parsePatchBody reads a JSON object whose keys are optional and
// distinguishes presence from nil so "rating": null clears the rating
// but an absent key leaves it alone.
func parsePatchBody(r io.Reader) (patchReq, error) {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return patchReq{}, fmt.Errorf("invalid JSON body: %w", err)
	}
	// json.Decoder.DisallowUnknownFields only applies to struct decoding,
	// not map decoding. Enforce the allowlist explicitly.
	for k := range raw {
		if k != "rating" && k != "status" && k != "review" {
			return patchReq{}, fmt.Errorf("unknown field %q", k)
		}
	}
	out := patchReq{}
	if v, ok := raw["rating"]; ok {
		out.ratingSet = true
		if string(v) != "null" {
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				return patchReq{}, fmt.Errorf("rating must be an integer or null")
			}
			out.rating = &n
		}
	}
	if v, ok := raw["status"]; ok {
		out.statusSet = true
		if err := json.Unmarshal(v, &out.status); err != nil {
			return patchReq{}, fmt.Errorf("status must be a string")
		}
	}
	if v, ok := raw["review"]; ok {
		out.reviewSet = true
		if err := json.Unmarshal(v, &out.review); err != nil {
			return patchReq{}, fmt.Errorf("review must be a string")
		}
	}
	return out, nil
}

// applyStatusSideEffects realizes the SKILL.md §Frontmatter state
// machine for status transitions:
//   - unread|dnf → reading : append today to started[]
//   - reading → finished   : append today to finished[]; bump read_count
//   - finished → reading   : append today to started[] (re-read)
//   - paused → reading     : no date change (resume in progress)
//   - reading → paused     : no date change
//   - reading → dnf        : no date append
//
// Every transition also appends a line to the Reading Timeline, giving
// the user a chronological narrative without manual bookkeeping.
func applyStatusSideEffects(fm *frontmatter.Frontmatter, b *body.Body, from, to string, today time.Time) {
	var timelineText string
	switch {
	case to == "reading" && (from == "unread" || from == "" || from == "dnf"):
		fm.AppendStarted(today)
		timelineText = "Started reading"
	case to == "reading" && from == "finished":
		fm.AppendStarted(today)
		timelineText = "Started re-read"
	case to == "reading" && from == "paused":
		timelineText = "Resumed"
	case to == "finished" && from != "finished":
		fm.AppendFinished(today)
		fm.SetReadCount(len(fm.Finished()))
		timelineText = "Finished"
	case to == "paused":
		timelineText = "Paused"
	case to == "dnf":
		timelineText = "DNF"
	default:
		timelineText = "Status: " + to
	}
	b.AppendTimelineEvent(today, timelineText)
}

// patchBookJSON projects the BookDetailView into a stable JSON shape.
// Pointer-typed numeric fields are emitted as number or null; slices
// are never nil so the frontend doesn't have to null-check.
func patchBookJSON(v BookDetailView) map[string]any {
	m := map[string]any{
		"filename":       v.Filename,
		"canonical_name": v.CanonicalName,
		"title":          v.Title,
		"subtitle":       v.Subtitle,
		"authors":        orEmpty(v.Authors),
		"categories":     orEmpty(v.Categories),
		"series_name":    v.SeriesName,
		"publisher":      v.Publisher,
		"publish_date":   v.PublishDate,
		"isbn":           v.ISBN,
		"cover":          v.Cover,
		"format":         v.Format,
		"source":         v.Source,
		"status":         v.Status,
		"read_count":     v.ReadCount,
		"started":        orEmpty(v.StartedDates),
		"finished":       orEmpty(v.FinishedDates),
		"review":         v.Review,
		"timeline":       orEmpty(v.TimelineLines),
	}
	m["rating"] = v.Rating
	m["total_pages"] = v.TotalPages
	m["series_index"] = v.SeriesIndex
	return m
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
