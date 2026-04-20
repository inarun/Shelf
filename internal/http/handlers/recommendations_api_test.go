package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		"shelf_axis_means", "length_mean", "length_stdev",
		"series_in_progress", "rating_mean", "book_count", "rated_count",
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

// addBookAndScan drops a markdown file into the books dir and re-runs
// FullScan so the SQLite index picks it up. Used by the recommendations
// tests that need fixtures beyond the single Hyperion seed.
func addBookAndScan(t *testing.T, d *Dependencies, booksAbs, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(booksAbs, filename), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestGetRecommendations_503WhenDisabled(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.GetRecommendations(rec,
		httptest.NewRequest(http.MethodGet, "/api/recommendations", nil))

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

func TestGetRecommendations_200WithRankedShape(t *testing.T) {
	d, books := seedDeps(t)
	d.RecommenderEnabled = true
	// Hyperion is status:reading and won't appear; seed an unread book
	// so the body contains at least one element with the documented shape.
	addBookAndScan(t, d, books, "Dune by Frank Herbert.md", `---
title: Dune
authors:
  - Frank Herbert
categories:
  - science-fiction
status: unread
---
# Dune
`)
	rec := httptest.NewRecorder()
	d.GetRecommendations(rec,
		httptest.NewRequest(http.MethodGet, "/api/recommendations", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var ranked []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ranked); err != nil {
		t.Fatalf("decode body: %v body=%s", err, rec.Body.String())
	}
	if len(ranked) == 0 {
		t.Fatalf("ranked is empty; body=%s", rec.Body.String())
	}
	wantKeys := []string{"filename", "title", "authors", "categories", "score", "reasons"}
	for _, k := range wantKeys {
		if _, ok := ranked[0][k]; !ok {
			t.Errorf("ranked[0] missing key %q; got %+v", k, ranked[0])
		}
	}
	if _, ok := ranked[0]["reasons"].([]any); !ok {
		t.Errorf("reasons must be a JSON array (got %T) so the SSR consumer can iterate without a null check",
			ranked[0]["reasons"])
	}
}

func TestGetRecommendations_FiltersToUnreadAndPaused(t *testing.T) {
	d, books := seedDeps(t)
	d.RecommenderEnabled = true
	addBookAndScan(t, d, books, "Dune by Frank Herbert.md", `---
title: Dune
authors:
  - Frank Herbert
status: unread
---
`)
	addBookAndScan(t, d, books, "Foundation by Isaac Asimov.md", `---
title: Foundation
authors:
  - Isaac Asimov
status: paused
---
`)
	addBookAndScan(t, d, books, "Lolita by Vladimir Nabokov.md", `---
title: Lolita
authors:
  - Vladimir Nabokov
status: finished
finished:
  - 2025-08-01
---
`)
	addBookAndScan(t, d, books, "Anna Karenina by Leo Tolstoy.md", `---
title: Anna Karenina
authors:
  - Leo Tolstoy
status: dnf
---
`)
	rec := httptest.NewRecorder()
	d.GetRecommendations(rec,
		httptest.NewRequest(http.MethodGet, "/api/recommendations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var ranked []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ranked); err != nil {
		t.Fatalf("decode body: %v body=%s", err, rec.Body.String())
	}
	gotTitles := map[string]bool{}
	for _, r := range ranked {
		gotTitles[r["title"].(string)] = true
	}
	if !gotTitles["Dune"] {
		t.Error("Dune (status: unread) should appear")
	}
	if !gotTitles["Foundation"] {
		t.Error("Foundation (status: paused) should appear")
	}
	if gotTitles["Hyperion"] {
		t.Error("Hyperion (status: reading) must NOT appear")
	}
	if gotTitles["Lolita"] {
		t.Error("Lolita (status: finished) must NOT appear")
	}
	if gotTitles["Anna Karenina"] {
		t.Error("Anna Karenina (status: dnf) must NOT appear")
	}
}
