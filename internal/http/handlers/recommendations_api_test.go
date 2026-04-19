package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetRecommendationsProfile_503WhenDisabled(t *testing.T) {
	d, _ := seedDeps(t)
	// d.RecommenderEnabled is false by zero-value.
	rec := httptest.NewRecorder()
	d.GetRecommendationsProfile(rec,
		httptest.NewRequest(http.MethodGet, "/api/recommendations/profile", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", rec.Code, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Error.Code != "unavailable" {
		t.Errorf("error.code = %q, want unavailable", env.Error.Code)
	}
}

func TestGetRecommendationsProfile_200WithProfileShape(t *testing.T) {
	d, _ := seedDeps(t)
	d.RecommenderEnabled = true
	rec := httptest.NewRecorder()
	d.GetRecommendationsProfile(rec,
		httptest.NewRequest(http.MethodGet, "/api/recommendations/profile", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode profile: %v body=%s", err, rec.Body.String())
	}
	// Confirm the full set of documented top-level keys is present. Any
	// rename or drop here is load-bearing for the S18 scorer contract.
	wantKeys := []string{
		"top_authors", "top_shelves", "axis_means", "axis_stdevs",
		"length_mean", "length_stdev", "series_in_progress",
		"rating_mean", "book_count", "rated_count",
	}
	for _, k := range wantKeys {
		if _, ok := body[k]; !ok {
			t.Errorf("profile JSON missing key %q; body=%s", k, rec.Body.String())
		}
	}

	// seedDeps seeds "Hyperion by Dan Simmons.md" with rating:4. Sanity-
	// check a couple of derived fields.
	if body["book_count"].(float64) != 1 {
		t.Errorf("book_count = %v, want 1", body["book_count"])
	}
	if body["rated_count"].(float64) != 1 {
		t.Errorf("rated_count = %v, want 1", body["rated_count"])
	}
	authors, ok := body["top_authors"].([]any)
	if !ok || len(authors) != 1 || authors[0] != "Dan Simmons" {
		t.Errorf("top_authors = %v, want [Dan Simmons]", body["top_authors"])
	}
}
