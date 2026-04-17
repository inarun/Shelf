// Package middleware provides the HTTP middleware chain for the Shelf
// server: request IDs, panic recovery, access logging, Host-header
// validation, CSP, session cookies, and CSRF. Each middleware is a
// pure http.Handler wrapper so ordering composes cleanly.
package middleware

import "net/http"

// Middleware wraps one handler with additional behavior. Composition
// happens via Chain.Then; declaration order reads outermost-first.
type Middleware func(http.Handler) http.Handler

// Chain is an ordered list of middlewares. Chain[0] is the outermost
// wrapper (sees the request first, writes last).
type Chain []Middleware

// Then returns a handler that runs mw[0] → mw[1] → ... → h.
// Implementation note: middlewares are applied in reverse so the
// caller's declaration order matches request-processing order.
func (c Chain) Then(h http.Handler) http.Handler {
	wrapped := h
	for i := len(c) - 1; i >= 0; i-- {
		wrapped = c[i](wrapped)
	}
	return wrapped
}
