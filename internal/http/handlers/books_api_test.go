package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustPatch(t *testing.T, d *Dependencies, filename, bodyJSON string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch,
		"/api/books/"+filename,
		strings.NewReader(bodyJSON),
	)
	req.SetPathValue("filename", filename)
	rec := httptest.NewRecorder()
	d.PatchBook(rec, req)
	return rec
}

func TestPatchRatingHappyPath(t *testing.T) {
	d, books := seedDeps(t)
	body := `{"rating": {"trial_system": {"emotional_impact": 5, "characters": 4, "plot": 5, "dialogue_prose": 3, "cinematography_worldbuilding": 5}, "overall": 6}}`
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(books, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "trial_system:") {
		t.Errorf("expected trial_system: key in file; content:\n%s", data)
	}
	if !strings.Contains(string(data), "overall: 6") {
		t.Errorf("expected overall: 6 in file; content:\n%s", data)
	}
	// Dual-write: body should carry a regenerated `## Rating` section.
	if !strings.Contains(string(data), "## Rating — ★ 6/5") {
		t.Errorf("expected managed `## Rating` body section; content:\n%s", data)
	}
}

func TestPatchRatingNullClears(t *testing.T) {
	d, books := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"rating": null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	data, err := os.ReadFile(filepath.Join(books, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "rating: 4") {
		t.Errorf("rating not cleared; content:\n%s", data)
	}
	if strings.Contains(string(data), "## Rating") {
		t.Errorf("body `## Rating` section should have been removed on null patch; content:\n%s", data)
	}
}

func TestPatchRatingRejectsLegacyScalar(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"rating": 5}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("legacy scalar rating: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"invalid"`) {
		t.Errorf("missing invalid error envelope: %s", rec.Body.String())
	}
}

func TestPatchRatingRejectsUnknownAxis(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md",
		`{"rating": {"trial_system": {"rogue_axis": 5}}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown axis: status = %d, want 400", rec.Code)
	}
}

func TestPatchRatingOverallOutOfRangeIs400(t *testing.T) {
	d, _ := seedDeps(t)
	for _, body := range []string{`{"rating": {"overall": 11}}`, `{"rating": {"overall": -1}}`} {
		rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestPatchStatusClobberUnreadIs400(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"status": "unread"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPatchStatusFinishedSideEffects(t *testing.T) {
	d, books := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"status": "finished"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(books, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Expect a finished date appended and read_count to be 1.
	if !strings.Contains(string(data), "read_count: 1") {
		t.Errorf("read_count not bumped; content:\n%s", data)
	}
	if !strings.Contains(string(data), "Finished") {
		t.Errorf("timeline did not gain Finished line; content:\n%s", data)
	}
	if !strings.Contains(string(data), "status: finished") {
		t.Errorf("status not persisted; content:\n%s", data)
	}
}

func TestPatchReviewReplacesNotes(t *testing.T) {
	d, books := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md",
		`{"review": "Entirely new review text."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(books, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Entirely new review text.") {
		t.Errorf("new review not present; content:\n%s", data)
	}
	if strings.Contains(string(data), "Dense first half") {
		t.Errorf("old review should have been replaced; content:\n%s", data)
	}
}

func TestPatch404OnUnknownFilename(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "Nonexistent.md", `{"rating": {"overall": 3}}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestPatch400OnTraversal(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "..%2Fetc.md", `{"rating": {"overall": 3}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPatch400OnInvalidJSON(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"rating":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPatch400OnUnknownField(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"rogue": 1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field should 400; got %d", rec.Code)
	}
}

func TestPatch400OnOversizedReview(t *testing.T) {
	d, _ := seedDeps(t)
	big := strings.Repeat("a", MaxReviewBytes+1)
	body, _ := json.Marshal(map[string]string{"review": big})
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized review: got %d; want 400", rec.Code)
	}
}

func TestPatchRespIncludesUpdatedBook(t *testing.T) {
	d, _ := seedDeps(t)
	body := `{"rating": {"trial_system": {"plot": 5}, "overall": 5}}`
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool `json:"ok"`
		Book struct {
			Filename string         `json:"filename"`
			Rating   map[string]any `json:"rating"`
			Status   string         `json:"status"`
			Review   string         `json:"review"`
		} `json:"book"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v body=%s", err, rec.Body.String())
	}
	if !resp.OK {
		t.Errorf("ok = false")
	}
	if resp.Book.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("book.filename = %q", resp.Book.Filename)
	}
	if resp.Book.Rating == nil {
		t.Fatalf("book.rating = nil, want map")
	}
	if ts, ok := resp.Book.Rating["trial_system"].(map[string]any); !ok || ts["plot"] != float64(5) {
		t.Errorf("book.rating.trial_system = %v, want plot=5", resp.Book.Rating["trial_system"])
	}
	if overall, ok := resp.Book.Rating["overall"].(float64); !ok || overall != 5 {
		t.Errorf("book.rating.overall = %v, want 5", resp.Book.Rating["overall"])
	}
}
