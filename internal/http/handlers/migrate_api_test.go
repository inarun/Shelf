package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyRatingFixture is a minimal note with a scalar `rating: 4`
// frontmatter — the exact shape migrate.BuildPlan flags for rewriting.
const legacyRatingFixture = `---
title: Foundation
authors:
  - Isaac Asimov
status: finished
rating: 4
---
# Foundation
`

// seedLegacyNote swaps the default hyperion fixture in the books dir
// for a legacy-scalar note and re-syncs. Returns the filename.
func seedLegacyNote(t *testing.T, d *Dependencies) string {
	t.Helper()
	// Clear the books folder first so FullScan doesn't find the default
	// Hyperion fixture.
	entries, err := os.ReadDir(d.BooksAbs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(d.BooksAbs, e.Name()))
	}
	filename := "Foundation by Isaac Asimov.md"
	if err := os.WriteFile(filepath.Join(d.BooksAbs, filename), []byte(legacyRatingFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return filename
}

// TestPlanMigrate_ReturnsPlanShape asserts the /api/migrate/plan
// handler returns the three-bucket Plan JSON envelope with
// will_migrate / will_skip / conflicts arrays.
func TestPlanMigrate_ReturnsPlanShape(t *testing.T) {
	d, _ := seedDeps(t)
	filename := seedLegacyNote(t, d)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/migrate/plan", nil)
	d.PlanMigrate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var plan map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v body=%s", err, rec.Body.String())
	}
	for _, key := range []string{"will_migrate", "will_skip", "conflicts"} {
		if _, ok := plan[key]; !ok {
			t.Errorf("plan missing bucket %q; body=%s", key, rec.Body.String())
		}
	}
	// The seeded legacy note must appear in will_migrate.
	wm, ok := plan["will_migrate"].([]any)
	if !ok || len(wm) != 1 {
		t.Fatalf("will_migrate = %v, want 1 entry", plan["will_migrate"])
	}
	entry := wm[0].(map[string]any)
	if entry["filename"] != filename {
		t.Errorf("entry.filename = %v, want %q", entry["filename"], filename)
	}
	if entry["old_value"] != float64(4) {
		t.Errorf("entry.old_value = %v, want 4", entry["old_value"])
	}
}

// TestApplyMigrate_HappyPath drives /api/migrate/apply end-to-end:
// seed a legacy-scalar note, hit the apply endpoint, assert the file
// on disk now carries the map-shape rating AND the `## Rating` body
// section (dual-write).
func TestApplyMigrate_HappyPath(t *testing.T) {
	d, _ := seedDeps(t)
	filename := seedLegacyNote(t, d)

	form := url.Values{}
	form.Set("decisions", "[]")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/migrate/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.ApplyMigrate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var report map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v body=%s", err, rec.Body.String())
	}
	migrated, _ := report["migrated"].([]any)
	if len(migrated) != 1 || migrated[0] != filename {
		t.Errorf("migrated = %v, want [%q]", migrated, filename)
	}
	if report["backup_root"] == "" {
		t.Errorf("backup_root empty; pre-apply snapshot should populate it")
	}

	// On-disk check: YAML must be map-shape (no bare scalar rating) and
	// body must carry the `## Rating` section. The post-migration YAML
	// form for a legacy scalar is `rating:\n  overall: N` — `trial_system`
	// is omitted when empty (SetRating only emits it when dimensioned).
	data, err := os.ReadFile(filepath.Join(d.BooksAbs, filename))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "\nrating: 4\n") {
		t.Errorf("YAML still carries legacy scalar; body:\n%s", got)
	}
	if !strings.Contains(got, "overall: 4") {
		t.Errorf("YAML missing overall: 4 after migrate; body:\n%s", got)
	}
	if !strings.Contains(got, "## Rating") {
		t.Errorf("body missing ## Rating section after migrate; body:\n%s", got)
	}
}

// TestApplyMigrate_RejectsInvalidDecision asserts the handler validates
// the decisions field shape before running BuildPlan, returning 400.
func TestApplyMigrate_RejectsInvalidDecision(t *testing.T) {
	d, _ := seedDeps(t)
	form := url.Values{}
	form.Set("decisions", `[{"filename":"x","action":"bogus"}]`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/migrate/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.ApplyMigrate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
