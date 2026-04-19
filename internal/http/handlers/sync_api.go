package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/inarun/Shelf/internal/http/middleware"
	"github.com/inarun/Shelf/internal/providers/reading/audiobookshelf"
)

// Pagination cap for ListeningSessions. 50 items/page (AB's default) ×
// 20 pages = 1000 sessions — far past any realistic v0.2 vault. A
// runaway AB instance cannot stall the sync pipeline forever.
const (
	syncSessionsPerPage = 50
	syncSessionsMaxPage = 20
)

// PlanSyncAudiobookshelf handles POST /api/sync/audiobookshelf/plan.
// Fetches the user's in-progress library items and recent listening
// sessions, builds a dry-run Plan against the vault, and returns it as
// JSON. No writes, no side effects.
//
// Returns 503 with the "unavailable" error code when the provider is
// disabled (config opt-out) — matches the Open Library posture, so
// clients can feature-gate on a single signal.
func (d *Dependencies) PlanSyncAudiobookshelf(w http.ResponseWriter, r *http.Request) {
	if d.AudiobookshelfClient == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "unavailable",
			"audiobookshelf sync is disabled in config")
		return
	}

	items, sessions, err := d.fetchAudiobookshelfState(r.Context())
	if err != nil {
		d.Logger.Error("audiobookshelf fetch",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusBadGateway, "server",
			"audiobookshelf fetch failed: "+err.Error())
		return
	}

	rv, err := audiobookshelf.NewResolver(r.Context(), d.Store)
	if err != nil {
		d.Logger.Error("audiobookshelf resolver",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "resolver init failed")
		return
	}

	plan, err := audiobookshelf.BuildPlan(r.Context(), items, sessions, rv, d.BooksAbs, time.Now().UTC())
	if err != nil {
		d.Logger.Error("audiobookshelf build plan",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "plan build failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(plan)
}

// ApplySyncAudiobookshelf handles POST /api/sync/audiobookshelf/apply.
// Stateless: the handler re-fetches AB state (same cost as /plan),
// re-builds the plan, promotes accepted conflicts via
// audiobookshelf.ApplyDecisions, then calls audiobookshelf.Apply which
// snapshots the Books folder first.
//
// Decision form field shape mirrors /api/import/apply:
//
//	decisions=[{"filename":"...","action":"accept"|"skip"}, ...]
//
// Missing / empty decisions are treated as all-skip. Returns 503 when
// disabled, matching PlanSyncAudiobookshelf.
func (d *Dependencies) ApplySyncAudiobookshelf(w http.ResponseWriter, r *http.Request) {
	if d.AudiobookshelfClient == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "unavailable",
			"audiobookshelf sync is disabled in config")
		return
	}

	// Cap body size — a rogue client cannot upload an unbounded decisions
	// string. Decisions is a short JSON array; MaxCSVBytes is overkill
	// but keeps the constant reused.
	if err := r.ParseForm(); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid",
			"could not parse form: "+err.Error())
		return
	}

	decisions, err := parseSyncDecisions(r.FormValue("decisions"))
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
		return
	}

	items, sessions, err := d.fetchAudiobookshelfState(r.Context())
	if err != nil {
		d.Logger.Error("audiobookshelf fetch",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusBadGateway, "server",
			"audiobookshelf fetch failed: "+err.Error())
		return
	}

	rv, err := audiobookshelf.NewResolver(r.Context(), d.Store)
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "resolver init failed")
		return
	}

	plan, err := audiobookshelf.BuildPlan(r.Context(), items, sessions, rv, d.BooksAbs, time.Now().UTC())
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "plan build failed")
		return
	}

	absDecisions := make([]audiobookshelf.Decision, 0, len(decisions))
	for _, gd := range decisions {
		absDecisions = append(absDecisions, audiobookshelf.Decision{
			Filename: gd.Filename,
			Action:   gd.Action,
		})
	}
	if err := audiobookshelf.ApplyDecisions(plan, absDecisions, d.BooksAbs); err != nil {
		d.Logger.Error("audiobookshelf apply decisions",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid",
			"apply decisions: "+err.Error())
		return
	}

	report, err := audiobookshelf.Apply(r.Context(), plan, d.BooksAbs, audiobookshelf.ApplyOptions{
		Syncer:      d.Syncer,
		BackupsRoot: d.BackupsRoot,
	})
	if err != nil {
		d.Logger.Error("audiobookshelf apply",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "apply failed: "+err.Error())
		return
	}

	d.Logger.Info("audiobookshelf sync applied",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"updated", len(report.Updated),
		"skipped", len(report.Skipped),
		"conflicts_in_plan", len(plan.Conflicts),
		"unmatched", len(plan.Unmatched),
		"decisions_received", len(decisions),
		"errors", len(report.Errors),
		"backup_root", report.BackupRoot,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(syncApplyReportJSON(report))
}

// fetchAudiobookshelfState consolidates the two client calls: in-
// progress items + paginated listening sessions (capped at
// syncSessionsMaxPage pages to bound network usage). Paging stops
// early when the server reports zero items or the returned page is
// short.
func (d *Dependencies) fetchAudiobookshelfState(ctx context.Context) ([]audiobookshelf.LibraryItem, []audiobookshelf.ListeningSession, error) {
	inProgress, err := d.AudiobookshelfClient.GetItemsInProgress(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("items-in-progress: %w", err)
	}
	var sessions []audiobookshelf.ListeningSession
	for page := 1; page <= syncSessionsMaxPage; page++ {
		resp, err := d.AudiobookshelfClient.GetListeningSessions(ctx, page, syncSessionsPerPage)
		if err != nil {
			return nil, nil, fmt.Errorf("listening-sessions page %d: %w", page, err)
		}
		if len(resp.Sessions) == 0 {
			break
		}
		sessions = append(sessions, resp.Sessions...)
		if resp.NumPages > 0 && page >= resp.NumPages {
			break
		}
		if len(resp.Sessions) < syncSessionsPerPage {
			break
		}
	}
	return inProgress.LibraryItems, sessions, nil
}

// parseSyncDecisions validates the decisions form-field shape.
// Empty / missing input returns nil, nil (treated as all-skip).
func parseSyncDecisions(raw string) ([]ConflictDecision, error) {
	if raw == "" {
		return nil, nil
	}
	var out []ConflictDecision
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decisions field: %w", err)
	}
	for i, d := range out {
		if d.Action != "accept" && d.Action != "skip" {
			return nil, fmt.Errorf("decisions[%d].action must be 'accept' or 'skip'", i)
		}
	}
	return out, nil
}

// syncApplyReportJSON flattens audiobookshelf.ApplyReport into the
// wire shape. Matches the Goodreads /import/apply response envelope.
func syncApplyReportJSON(r *audiobookshelf.ApplyReport) map[string]any {
	errs := make([]map[string]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		errs = append(errs, map[string]string{
			"filename": e.Filename,
			"phase":    e.Phase,
			"error":    e.Err.Error(),
		})
	}
	return map[string]any{
		"backup_root": r.BackupRoot,
		"updated":     orEmptyStrings(r.Updated),
		"skipped":     orEmptyStrings(r.Skipped),
		"errors":      errs,
	}
}

