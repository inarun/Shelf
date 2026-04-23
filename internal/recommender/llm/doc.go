// Package llm is the opt-in Anthropic API client that powers the v0.4
// LLM-enhanced recommender (SKILL.md §v0.4). It is the outbound side of
// the "Tune with LLM" affordance the user clicks on /recommendations
// (Session 24); nothing in this package initiates traffic unless the
// user explicitly opts in via [recommender.llm] in shelf.toml AND
// triggers the tune flow from the UI.
//
// Security posture mirrors internal/providers/reading/audiobookshelf
// with two deliberate differences: the timeout is 30s (LLM responses
// take real time) and the auth header is Anthropic-native x-api-key
// rather than Authorization: Bearer.
//
//   - 30s request timeout on every outbound call.
//   - 512 KiB cap on JSON response bodies.
//   - application/json content-type allowlist — any other content-type
//     (e.g., an upstream HTML error page) is rejected outright.
//   - Same-host redirect policy: redirects to a host other than the
//     configured base URL are rejected. Redirect chains are capped at 3.
//   - x-api-key header carries the user's Anthropic key; the key is
//     NEVER written into log lines or error strings. Error formatting
//     uses a redacted URL (path only, no query string).
//   - anthropic-version header pins the API version this client speaks.
//   - Model IDs are validated against a startup allowlist in
//     internal/config.validateRecommender; a misconfigured model never
//     reaches this package (SKILL.md §Conventions for Claude Code).
//
// Scope for Session 22: client constructor, auth, redactedURL, the
// Ping(ctx) health check against GET /v1/models, and the TunedWeights
// persistence layer (Load/Save backed by internal/vault/atomic.Write).
// Session 23 adds the prompt assembly and the Tune algorithm. Session
// 24 wires the /recommendations UI and the /api/recommendations/tune
// endpoint.
package llm
