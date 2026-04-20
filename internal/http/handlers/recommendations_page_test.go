package handlers

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestRecommendationsPageRendersRanked seeds a finished+rated book
// plus an unread candidate, then asserts the SSR page renders the
// candidate with a Why? disclosure and at least one reason list item.
func TestRecommendationsPageRendersRanked(t *testing.T) {
	d, books := seedDeps(t)
	d.RecommenderEnabled = true
	// Hyperion is pre-seeded by seedDeps (status:reading — filtered out).
	// Seed an unread candidate; the shared author/category with a rated
	// book would help scoring, but the ranker returns all candidates,
	// just with varying scores — so a single unread book is enough for
	// the render guard.
	addBookAndScan(t, d, books, "Ilium by Dan Simmons.md", `---
title: Ilium
authors:
  - Dan Simmons
categories:
  - science-fiction
status: unread
---
# Ilium
`)
	rec := httptest.NewRecorder()
	d.RecommendationsPage(rec,
		httptest.NewRequest(http.MethodGet, "/recommendations", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Ilium") {
		t.Errorf("body missing unread candidate title; body:\n%s", body)
	}
	if !strings.Contains(body, `data-recommendations`) {
		t.Errorf("body missing data-recommendations container; body:\n%s", body)
	}
	if !strings.Contains(body, `data-why-toggle`) {
		t.Errorf("body missing why-toggle button; body:\n%s", body)
	}
	if !strings.Contains(body, `aria-expanded="false"`) {
		t.Errorf("why-toggle must start collapsed; body:\n%s", body)
	}
	if !strings.Contains(body, `class="reason-list"`) {
		t.Errorf("body missing .reason-list for the Why? panel; body:\n%s", body)
	}
}

// TestRecommendationsEmptyStateRendered asserts the enabled-but-empty
// branch renders the empty-state illustration and no grid container.
// seedDeps's single Hyperion is status:reading (filtered out), so the
// default library produces zero candidates.
func TestRecommendationsEmptyStateRendered(t *testing.T) {
	d, _ := seedDeps(t)
	d.RecommenderEnabled = true
	rec := httptest.NewRecorder()
	d.RecommendationsPage(rec,
		httptest.NewRequest(http.MethodGet, "/recommendations", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No recommendations yet") {
		t.Errorf("empty-state title missing; body:\n%s", body)
	}
	if !strings.Contains(body, `class="empty-state__icon"`) {
		t.Errorf("empty-state illustration missing; body:\n%s", body)
	}
	if strings.Contains(body, `class="recommendations-grid"`) {
		t.Errorf("empty branch must not render the grid; body:\n%s", body)
	}
}

// TestRecommendationsDisabledBannerRendered asserts the disabled branch
// renders the warn banner and no grid. Status must still be 200 so the
// URL is bookmarkable (per Session 19 design decision).
func TestRecommendationsDisabledBannerRendered(t *testing.T) {
	d, _ := seedDeps(t)
	// d.RecommenderEnabled is false by zero-value.
	rec := httptest.NewRecorder()
	d.RecommendationsPage(rec,
		httptest.NewRequest(http.MethodGet, "/recommendations", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="banner warn"`) {
		t.Errorf("disabled branch must render .banner.warn; body:\n%s", body)
	}
	if !strings.Contains(body, "Recommendations are disabled") {
		t.Errorf("disabled branch must explain the disabled state; body:\n%s", body)
	}
	if strings.Contains(body, `class="recommendations-grid"`) {
		t.Errorf("disabled branch must not render the grid; body:\n%s", body)
	}
	if strings.Contains(body, `data-why-toggle`) {
		t.Errorf("disabled branch must not render Why? toggles; body:\n%s", body)
	}
}

// TestWhyPopoverDisclosure asserts the HTML contract the delegated JS
// listener in initRecommendations depends on: for every why-toggle
// button, there is a matching hidden popover with id == aria-controls.
// If this contract slips, the toggle will point at nothing and the JS
// will silently no-op.
func TestWhyPopoverDisclosure(t *testing.T) {
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
	rec := httptest.NewRecorder()
	d.RecommendationsPage(rec,
		httptest.NewRequest(http.MethodGet, "/recommendations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	btnRx := regexp.MustCompile(`<button[^>]*\bdata-why-toggle\b[^>]*\baria-controls="([^"]+)"`)
	btnMatches := btnRx.FindAllStringSubmatch(body, -1)
	if len(btnMatches) < 2 {
		t.Fatalf("expected >=2 why-toggle buttons; got %d; body:\n%s", len(btnMatches), body)
	}
	for _, m := range btnMatches {
		id := m[1]
		if !strings.Contains(body, `id="`+id+`"`) {
			t.Errorf("why-toggle aria-controls=%q has no target element; body:\n%s", id, body)
		}
		panelRx := regexp.MustCompile(`<div[^>]*\bclass="why-popover"[^>]*\bid="` + regexp.QuoteMeta(id) + `"[^>]*\bhidden\b`)
		if !panelRx.MatchString(body) {
			t.Errorf("expected hidden .why-popover with id=%q; body:\n%s", id, body)
		}
	}
}

// TestKbdHelpListsRecommendationsShortcut asserts the help overlay
// documents the `g r` chord. The chord is registered unconditionally
// (per Session 19 design decision), so this guard fires on any page;
// /library is convenient.
func TestKbdHelpListsRecommendationsShortcut(t *testing.T) {
	d, _ := seedDeps(t)
	rec := httptest.NewRecorder()
	d.LibraryView(rec, httptest.NewRequest(http.MethodGet, "/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// The overlay <dd> lives in _shared.html. A loose Contains pair is
	// enough — the `g m` and `?` entries above it guarantee structure.
	if !strings.Contains(body, "Go to Recommendations") {
		t.Errorf("help overlay missing 'Go to Recommendations'; body:\n%s", body)
	}
	rx := regexp.MustCompile(`<kbd>g</kbd>\s*<kbd>r</kbd>`)
	if !rx.MatchString(body) {
		t.Errorf("help overlay missing <kbd>g</kbd> <kbd>r</kbd> chord; body:\n%s", body)
	}
}

// TestNavShowsRecommendationsWhenEnabled asserts the nav Recommendations
// entry appears on any page when RecommenderEnabled is true. Uses
// /library for convenience.
func TestNavShowsRecommendationsWhenEnabled(t *testing.T) {
	d, _ := seedDeps(t)
	d.RecommenderEnabled = true
	rec := httptest.NewRecorder()
	d.LibraryView(rec, httptest.NewRequest(http.MethodGet, "/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `href="/recommendations"`) {
		t.Errorf("nav missing Recommendations link when enabled; body:\n%s", body)
	}
}

// TestNavHidesRecommendationsWhenDisabled asserts the nav Recommendations
// entry is absent when RecommenderEnabled is false. The chord stays
// registered (per design decision), but the nav link is gated so the
// disabled feature isn't click-discoverable.
func TestNavHidesRecommendationsWhenDisabled(t *testing.T) {
	d, _ := seedDeps(t)
	// d.RecommenderEnabled is false by zero-value.
	rec := httptest.NewRecorder()
	d.LibraryView(rec, httptest.NewRequest(http.MethodGet, "/library", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `href="/recommendations"`) {
		t.Errorf("nav must not link to /recommendations when disabled; body:\n%s", body)
	}
}
