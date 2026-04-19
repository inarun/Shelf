package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/inarun/Shelf/internal/http/middleware"
	"github.com/inarun/Shelf/internal/vault/migrate"
)

// PlanMigrate handles POST /api/migrate/plan. Runs migrate.BuildPlan
// against the current index + vault and returns the plan as JSON.
// Stateless — re-running produces the same Plan modulo mtime churn.
func (d *Dependencies) PlanMigrate(w http.ResponseWriter, r *http.Request) {
	plan, err := migrate.BuildPlan(r.Context(), d.Store, d.BooksAbs)
	if err != nil {
		d.Logger.Error("migrate build plan",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server",
			"plan build failed: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(plan)
}

// ApplyMigrate handles POST /api/migrate/apply. Re-builds the plan
// (stateless, same cost as /plan), threads the decisions form field
// through migrate.ApplyDecisions, then runs migrate.Apply which
// snapshots the Books folder and rewrites each note atomically.
//
// The decisions form field follows the same shape as /api/import/apply
// and /api/sync/audiobookshelf/apply:
//
//	decisions=[{"filename":"...","action":"accept"|"skip"}, ...]
//
// v0.2.1 produces no conflicts semantically, so decisions currently
// have no effect on the outcome — the field exists for wire symmetry.
func (d *Dependencies) ApplyMigrate(w http.ResponseWriter, r *http.Request) {
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

	plan, err := migrate.BuildPlan(r.Context(), d.Store, d.BooksAbs)
	if err != nil {
		d.Logger.Error("migrate build plan",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server",
			"plan build failed: "+err.Error())
		return
	}

	mdec := make([]migrate.Decision, 0, len(decisions))
	for _, d := range decisions {
		mdec = append(mdec, migrate.Decision{
			Filename: d.Filename,
			Action:   d.Action,
		})
	}
	if err := migrate.ApplyDecisions(plan, mdec); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid",
			"apply decisions: "+err.Error())
		return
	}

	report, err := migrate.Apply(r.Context(), plan, d.BooksAbs, migrate.ApplyOptions{
		Syncer:      d.Syncer,
		BackupsRoot: d.BackupsRoot,
	})
	if err != nil {
		d.Logger.Error("migrate apply",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server",
			"apply failed: "+err.Error())
		return
	}

	d.Logger.Info("migrate applied",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"migrated", len(report.Migrated),
		"skipped", len(report.Skipped),
		"conflicts_in_plan", len(plan.Conflicts),
		"decisions_received", len(decisions),
		"errors", len(report.Errors),
		"backup_root", report.BackupRoot,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(migrateApplyReportJSON(report))
}

// migrateApplyReportJSON flattens migrate.ApplyReport into the wire
// shape. Matches /api/sync/audiobookshelf/apply's response envelope so
// the app.js renderSyncReport helper can render it without a second
// implementation.
func migrateApplyReportJSON(r *migrate.ApplyReport) map[string]any {
	errs := make([]map[string]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		errs = append(errs, map[string]string{
			"filename": e.Filename,
			"phase":    e.Phase,
			"error":    e.Message,
		})
	}
	return map[string]any{
		"backup_root": r.BackupRoot,
		"migrated":    orEmptyStrings(r.Migrated),
		"skipped":     orEmptyStrings(r.Skipped),
		"errors":      errs,
	}
}
