// Package audiobookshelf is a read-only client for a self-hosted
// Audiobookshelf server. It is the data source for the v0.2 sync flow
// (SKILL.md §v0.2).
//
// Security posture mirrors internal/providers/metadata/openlibrary:
//
//   - 15s request timeout on every outbound call.
//   - 5 MiB cap on JSON response bodies (listening-sessions pages can be
//     large, but 5 MiB is still conservative headroom).
//   - application/json content-type allowlist — any other content-type
//     (e.g., a reverse-proxy HTML error page) is rejected outright.
//   - Same-host redirect policy: redirects to a host other than the
//     configured base URL are rejected. Redirect chains are capped at 3.
//   - The API key is bearer-style (Authorization: Bearer <key>) and is
//     NEVER written into log lines or error strings. Error formatting
//     uses a redacted URL (path only, no query string).
//
// Scope for Session 12: GetMe, GetItemsInProgress, GetListeningSessions.
// Session 13 adds the mapper/plan/apply layer that consumes these.
// Session 14 wires the /sync UI.
package audiobookshelf
