package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/inarun/Shelf/internal/domain/series"
	"github.com/inarun/Shelf/internal/http/middleware"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
	"github.com/inarun/Shelf/internal/recommender/rules"
)

// maxRecommendations caps the response size so a multi-thousand-book
// library doesn't drown the SSR card grid Session 19 will render. 50 is
// generous enough for the user to scroll past the obviously-uninteresting
// long-tail without paginating.
const maxRecommendations = 50

// GetRecommendationsProfile handles GET /api/recommendations/profile.
// Returns the taste profile derived from the index as JSON. This is a
// debug / introspection endpoint: Session 18 will consume Profile
// internally via the scorers and Session 19 will drive the UI off a
// separate /api/recommendations JSON endpoint.
//
// Gated on [recommender].enabled in config — 503 with code
// "unavailable" when the user has not opted in. Mirrors the Open
// Library and Audiobookshelf postures so the frontend can feature-gate
// on a single signal.
func (d *Dependencies) GetRecommendationsProfile(w http.ResponseWriter, r *http.Request) {
	if !d.RecommenderEnabled {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "unavailable",
			"recommender is disabled in config")
		return
	}

	p, err := profile.Extract(r.Context(), d.Store)
	if err != nil {
		d.Logger.Error("recommender profile extract",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server",
			"profile extract failed")
		return
	}

	d.Logger.Debug("recommender profile served",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"books", p.BookCount,
		"rated", p.RatedCount,
		"series_in_progress", len(p.SeriesInProgress),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(p)
}

// GetRecommendations handles GET /api/recommendations. Returns up to
// maxRecommendations ScoredBook entries, ranked by combined score, JSON-
// encoded as a bare array. Candidate eligibility: status ∈ {"", "unread",
// "paused"}. Walks the index once and reuses the books slice for both
// Profile.Build and the candidate filter.
//
// Gated on [recommender].enabled — same 503/"unavailable" envelope as
// GetRecommendationsProfile.
func (d *Dependencies) GetRecommendations(w http.ResponseWriter, r *http.Request) {
	if !d.RecommenderEnabled {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "unavailable",
			"recommender is disabled in config")
		return
	}

	books, err := d.Store.ListBooks(r.Context(), store.Filter{})
	if err != nil {
		d.Logger.Error("recommender list books",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server",
			"list books failed")
		return
	}

	p := profile.Build(books, time.Now().UTC())
	ss := series.Detect(books)

	candidates := make([]store.BookRow, 0, len(books))
	for _, b := range books {
		switch b.Status {
		case "", "unread", "paused":
			candidates = append(candidates, b)
		}
	}

	ranked := rules.Rank(candidates, p, ss, rules.DefaultWeights)
	if len(ranked) > maxRecommendations {
		ranked = ranked[:maxRecommendations]
	}

	d.Logger.Debug("recommendations served",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"library", len(books),
		"candidates", len(candidates),
		"returned", len(ranked),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ranked)
}
