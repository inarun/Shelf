package handlers

import (
	"context"
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
// encoded as a bare array.
//
// Gated on [recommender].enabled — same 503/"unavailable" envelope as
// GetRecommendationsProfile.
func (d *Dependencies) GetRecommendations(w http.ResponseWriter, r *http.Request) {
	if !d.RecommenderEnabled {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "unavailable",
			"recommender is disabled in config")
		return
	}

	ranked, _, err := d.rankRecommendations(r.Context())
	if err != nil {
		d.Logger.Error("recommender list books",
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		d.writeJSONError(w, r, http.StatusInternalServerError, "server",
			"list books failed")
		return
	}

	d.Logger.Debug("recommendations served",
		"request_id", middleware.RequestIDFrom(r.Context()),
		"returned", len(ranked),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ranked)
}

// rankRecommendations is the shared pipeline behind both the JSON
// /api/recommendations endpoint and the SSR /recommendations page. It
// walks the index once, builds the taste profile + series states, filters
// the candidates (status ∈ {"", "unread", "paused"} — reading/finished/
// dnf excluded), runs rules.Rank with DefaultWeights, and truncates to
// maxRecommendations before returning.
//
// The second return is a filename→BookRow lookup over the candidate
// slice so SSR callers can zip back to the full row (Cover, RatingOverall,
// StartedDates, …) without a second store scan. JSON callers discard it.
//
// No caller-facing status gate here — callers own the 503 envelope
// (JSON) or the disabled-banner render (SSR).
func (d *Dependencies) rankRecommendations(ctx context.Context) ([]rules.ScoredBook, map[string]store.BookRow, error) {
	books, err := d.Store.ListBooks(ctx, store.Filter{})
	if err != nil {
		return nil, nil, err
	}

	p := profile.Build(books, time.Now().UTC())
	ss := series.Detect(books)

	candidates := make([]store.BookRow, 0, len(books))
	byFilename := make(map[string]store.BookRow, len(books))
	for _, b := range books {
		switch b.Status {
		case "", "unread", "paused":
			candidates = append(candidates, b)
			byFilename[b.Filename] = b
		}
	}

	ranked := rules.Rank(candidates, p, ss, rules.DefaultWeights)
	if len(ranked) > maxRecommendations {
		ranked = ranked[:maxRecommendations]
	}

	return ranked, byFilename, nil
}
