package llm

import "time"

// Package-local constants mirror the security posture documented in
// doc.go. Callers in cmd/shelf read shelf.toml and hand in the bits
// via Config; these constants are not configurable by design — varying
// them per-deployment complicates audit and encourages drift.
const (
	userAgent        = "Shelf/0.1 (+https://github.com/inarun/Shelf)"
	anthropicAPIBase = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"

	// jsonMaxBytes caps a single response body. /v1/models and future
	// tune responses both fit comfortably under 512 KiB.
	jsonMaxBytes = 512 * 1024

	// requestTimeout bounds every outbound call. LLM responses take
	// longer than typical metadata provider calls, so the budget is
	// 30s rather than audiobookshelf/openlibrary's 15s.
	requestTimeout = 30 * time.Second

	maxRedirects = 3
)

// Config is what New needs to build a Client. BaseURL defaults to
// the public Anthropic API when empty; tests override it to point at
// an httptest.Server. APIKey and Model are the per-user values sourced
// from [recommender.llm] in shelf.toml. Model is assumed already
// validated against the allowlist by internal/config; this package
// treats it as opaque text.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}
