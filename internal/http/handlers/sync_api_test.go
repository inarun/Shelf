package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/providers/reading/audiobookshelf"
)

// fakeAB spins up an httptest.Server that mimics the three endpoints
// our sync pipeline calls: /api/me, /api/me/items-in-progress, and
// /api/me/listening-sessions. Reuses the JSON fixtures committed under
// internal/providers/reading/audiobookshelf/testdata/.
func fakeAB(t *testing.T) *httptest.Server {
	t.Helper()
	root, err := filepath.Abs("../../providers/reading/audiobookshelf/testdata")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var filename string
		switch r.URL.Path {
		case "/api/me":
			filename = "me.json"
		case "/api/me/items-in-progress":
			filename = "items_in_progress.json"
		case "/api/me/listening-sessions":
			filename = "listening_sessions.json"
		default:
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withABClient returns a Dependencies bundle seeded by seedDeps, with
// an audiobookshelf.Client wired against fakeAB. Without this, Plan and
// Apply handlers return 503.
func withABClient(t *testing.T, d *Dependencies) {
	t.Helper()
	srv := fakeAB(t)
	c, err := audiobookshelf.New(audiobookshelf.Credentials{
		BaseURL: srv.URL,
		APIKey:  "fake-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.AudiobookshelfClient = c
}

func TestSyncAudiobookshelfPlan_Returns503WhenDisabled(t *testing.T) {
	d, _ := seedDeps(t)
	// d.AudiobookshelfClient is nil by default.
	rec := httptest.NewRecorder()
	d.PlanSyncAudiobookshelf(rec, httptest.NewRequest(http.MethodPost, "/api/sync/audiobookshelf/plan", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Error.Code != "unavailable" {
		t.Errorf("error code = %q, want unavailable", env.Error.Code)
	}
}

func TestSyncAudiobookshelfApply_Returns503WhenDisabled(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.ApplySyncAudiobookshelf(rec, httptest.NewRequest(http.MethodPost, "/api/sync/audiobookshelf/apply", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}
}

func TestSyncAudiobookshelfPlan_ReturnsPlanShape(t *testing.T) {
	d, _ := seedDeps(t)
	withABClient(t, d)

	rec := httptest.NewRecorder()
	d.PlanSyncAudiobookshelf(rec, httptest.NewRequest(http.MethodPost, "/api/sync/audiobookshelf/plan", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"will_update"`, `"conflicts"`, `"will_skip"`, `"unmatched"`} {
		if !strings.Contains(body, want) {
			t.Errorf("plan body missing %q; got:\n%s", want, body)
		}
	}
}

func TestSyncAudiobookshelfApply_RejectsInvalidDecision(t *testing.T) {
	d, _ := seedDeps(t)
	withABClient(t, d)

	form := url.Values{}
	form.Set("decisions", `[{"filename":"X","action":"bogus"}]`)
	req := httptest.NewRequest(http.MethodPost, "/api/sync/audiobookshelf/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	d.ApplySyncAudiobookshelf(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSyncAudiobookshelfApply_HappyPath(t *testing.T) {
	d, _ := seedDeps(t)
	withABClient(t, d)

	// Empty decisions = accept nothing, just apply non-conflict updates.
	req := httptest.NewRequest(http.MethodPost, "/api/sync/audiobookshelf/apply", http.NoBody)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	d.ApplySyncAudiobookshelf(rec, req)

	if rec.Code != http.StatusOK {
		raw, _ := io.ReadAll(rec.Body)
		t.Fatalf("status = %d body=%s", rec.Code, string(raw))
	}
	body := rec.Body.String()
	for _, want := range []string{`"backup_root"`, `"updated"`, `"skipped"`, `"errors"`} {
		if !strings.Contains(body, want) {
			t.Errorf("apply report missing %q; got:\n%s", want, body)
		}
	}
}
