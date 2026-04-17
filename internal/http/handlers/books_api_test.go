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
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"rating": 5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Assert frontmatter on disk actually changed.
	data, err := os.ReadFile(filepath.Join(books, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "rating: 5") {
		t.Errorf("expected rating: 5 in file; content:\n%s", data)
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
}

func TestPatchRatingOutOfRangeIs400(t *testing.T) {
	d, _ := seedDeps(t)
	for _, body := range []string{`{"rating": 0}`, `{"rating": 6}`, `{"rating": -1}`} {
		rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s: status = %d, want 400", body, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"invalid"`) {
			t.Errorf("body=%s: missing invalid error envelope", body)
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
	rec := mustPatch(t, d, "Nonexistent.md", `{"rating": 3}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestPatch400OnTraversal(t *testing.T) {
	d, _ := seedDeps(t)
	rec := mustPatch(t, d, "..%2Fetc.md", `{"rating": 3}`)
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
	rec := mustPatch(t, d, "Hyperion%20by%20Dan%20Simmons.md", `{"rating": 5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		OK   bool `json:"ok"`
		Book struct {
			Filename string  `json:"filename"`
			Rating   float64 `json:"rating"`
			Status   string  `json:"status"`
			Review   string  `json:"review"`
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
	if int(resp.Book.Rating) != 5 {
		t.Errorf("book.rating = %v, want 5", resp.Book.Rating)
	}
}
