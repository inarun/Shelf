package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/inarun/Shelf/internal/http/middleware"
	"github.com/inarun/Shelf/internal/providers/reading/goodreads"
)

// ConflictDecision is a single entry in the `decisions` form field
// submitted alongside the CSV on Apply. "accept" asks the importer to
// treat a flagged conflict as an update; "skip" leaves it unwritten.
//
// v0.1 note: the goodreads resolver does not yet have a promote-
// borderline-match mode, so "accept" is currently treated as "skip".
// The wire format is stable — future sessions can add promotion
// without an API change.
type ConflictDecision struct {
	Filename string `json:"filename"`
	Action   string `json:"action"`
}

// PlanImport handles POST /api/import/plan. The CSV is consumed once,
// a dry-run plan is computed, and the Plan is returned as JSON. No
// writes, no side effects.
func (d *Dependencies) PlanImport(w http.ResponseWriter, r *http.Request) {
	records, err := d.parseCSVFromMultipart(w, r)
	if err != nil {
		// parseCSVFromMultipart wrote the error; nothing to add.
		return
	}

	resolver, err := goodreads.NewResolver(r.Context(), d.Store)
	if err != nil {
		d.Logger.Error("new resolver",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "resolver init failed")
		return
	}

	plan, err := goodreads.BuildPlan(r.Context(), records, resolver, d.BooksAbs, time.Now().UTC())
	if err != nil {
		d.Logger.Error("build plan",
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

// ApplyImport handles POST /api/import/apply. Stateless: the client
// re-submits the same CSV alongside `decisions`. The handler re-builds
// the plan, filters conflict entries per decisions (v0.1: all conflicts
// are effectively skipped — see ConflictDecision doc), and runs
// goodreads.Apply which snapshots the Books folder first.
func (d *Dependencies) ApplyImport(w http.ResponseWriter, r *http.Request) {
	records, err := d.parseCSVFromMultipart(w, r)
	if err != nil {
		return
	}

	// Decisions field is optional; missing = all-skip.
	decisions, err := parseDecisions(r.FormValue("decisions"))
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	_ = decisions // acknowledged, logged below; not consumed in v0.1.

	resolver, err := goodreads.NewResolver(r.Context(), d.Store)
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "resolver init failed")
		return
	}
	plan, err := goodreads.BuildPlan(r.Context(), records, resolver, d.BooksAbs, time.Now().UTC())
	if err != nil {
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "plan build failed")
		return
	}

	report, err := goodreads.Apply(r.Context(), plan, d.BooksAbs, goodreads.ApplyOptions{
		Syncer:      d.Syncer,
		ImportStamp: time.Now().UTC(),
		BackupsRoot: d.BackupsRoot,
	})
	if err != nil {
		// Only the pre-apply backup failure returns non-nil; per-entry
		// errors end up in report.Errors.
		d.Logger.Error("apply import",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server", "apply failed: "+err.Error())
		return
	}

	d.Logger.Info("import applied",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"created", len(report.Created),
		"updated", len(report.Updated),
		"skipped", len(report.Skipped),
		"conflicts_in_plan", len(plan.Conflicts),
		"decisions_received", len(decisions),
		"errors", len(report.Errors),
		"backup_root", report.BackupRoot,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(applyReportJSON(report))
}

// parseCSVFromMultipart caps the request body, parses multipart, reads
// the `csv` file field, and parses it through the goodreads reader.
// On failure, writes the appropriate JSON error and returns a non-nil
// error — the caller should return immediately.
func (d *Dependencies) parseCSVFromMultipart(w http.ResponseWriter, r *http.Request) ([]goodreads.Record, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxCSVBytes)
	// #nosec G120 -- Body is capped via MaxBytesReader on the line above,
	// so ParseMultipartForm cannot consume more than MaxCSVBytes. gosec
	// flags the pattern on pattern match without seeing the cap.
	if err := r.ParseMultipartForm(MaxCSVBytes); err != nil {
		// Distinguish "too large" from "malformed".
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			d.writeJSONError(w, r, http.StatusRequestEntityTooLarge, "invalid",
				fmt.Sprintf("upload exceeds %d bytes", MaxCSVBytes))
			return nil, err
		}
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "could not parse multipart form: "+err.Error())
		return nil, err
	}

	file, header, err := r.FormFile("csv")
	if err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "missing 'csv' file field")
		return nil, err
	}
	defer func() { _ = file.Close() }()

	if header != nil && header.Size > MaxCSVBytes {
		d.writeJSONError(w, r, http.StatusRequestEntityTooLarge, "invalid",
			fmt.Sprintf("csv file exceeds %d bytes", MaxCSVBytes))
		return nil, errors.New("csv oversized")
	}

	// Drain into a buffered reader so the goodreads parser sees a
	// deterministic stream (multipart file readers can be stateful).
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "could not read csv body: "+err.Error())
		return nil, err
	}

	reader := goodreads.NewReader(&buf)
	reader.SetMaxTotalBytes(MaxCSVBytes)
	records, parseErr := reader.ReadAll()
	if parseErr != nil && len(records) == 0 {
		// No records recovered at all — hard failure.
		d.writeJSONError(w, r, http.StatusBadRequest, "invalid", "csv parse: "+parseErr.Error())
		return nil, parseErr
	}
	// parseErr may be non-nil but with some records recovered — we keep
	// the recovered records and surface per-row errors into the plan's
	// conflicts (BuildPlan already handles missing-identity cases).
	return records, nil
}

func parseDecisions(raw string) ([]ConflictDecision, error) {
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

// applyReportJSON flattens goodreads.ApplyReport into the wire shape.
func applyReportJSON(r *goodreads.ApplyReport) map[string]any {
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
		"created":     orEmptyStrings(r.Created),
		"updated":     orEmptyStrings(r.Updated),
		"skipped":     orEmptyStrings(r.Skipped),
		"errors":      errs,
	}
}

func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
