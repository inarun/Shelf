package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/inarun/Shelf/internal/http/middleware"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

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
