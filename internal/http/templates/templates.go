// Package templates embeds the SSR HTML templates and provides a parsed
// *template.Template with a small set of funcmap helpers for rendering
// ratings, status pills, and safe per-book URLs. No inline scripts are
// allowed in any template — the CSP forbids them.
package templates

import (
	"embed"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

//go:embed *.html
var files embed.FS

// FuncMap is exposed so tests can assert the registered helpers.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"join":        func(sep string, a []string) string { return strings.Join(a, sep) },
		"stars":       stars,
		"statusClass": statusClass,
		"statusLabel": statusLabel,
		"safeHref":    safeHref,
		"pluralS":     func(n int) string { if n == 1 { return "" }; return "s" },
		"emptyOr":     func(s, fallback string) string { if s == "" { return fallback }; return s },
		"deref":       derefInt,
	}
}

// Parse parses every embedded *.html file into one *template.Template
// set. Each page defines its own top-level template name (library,
// book_detail, import, error); _shared.html provides the "head" and
// "nav" partials they all pull in.
func Parse() (*template.Template, error) {
	t := template.New("shelf").Funcs(FuncMap())
	return t.ParseFS(files, "*.html")
}

// FS returns the embedded template filesystem for tests that want to
// walk the raw files (e.g., the "no inline script" guard).
func FS() embed.FS { return files }

func stars(r any) string {
	n, ok := ratingInt(r)
	if !ok {
		return "—"
	}
	if n < 1 || n > 5 {
		return "—"
	}
	return strings.Repeat("★", n) + strings.Repeat("☆", 5-n)
}

func ratingInt(r any) (int, bool) {
	switch v := r.(type) {
	case int:
		return v, true
	case *int:
		if v == nil {
			return 0, false
		}
		return *v, true
	case int64:
		return int(v), true
	case *int64:
		if v == nil {
			return 0, false
		}
		return int(*v), true
	}
	return 0, false
}

func derefInt(v any) int {
	n, _ := ratingInt(v)
	return n
}

func statusClass(s string) string {
	switch s {
	case "reading", "finished", "paused", "dnf", "unread":
		return "status-" + s
	}
	return "status-unread"
}

func statusLabel(s string) string {
	if s == "" {
		return "unread"
	}
	return s
}

// safeHref returns a URL-safe path-escaped link to /books/{filename}.
// The returned type is template.URL so html/template does not double-
// escape it; the input is filename-only, already validated upstream.
//
// #nosec G203 -- We deliberately bypass html/template's auto-escaping
// for the href value because url.PathEscape has already encoded every
// unsafe character. The input comes from store rows that were themselves
// populated from vault filenames validated by internal/vault/paths;
// attacker-controlled content cannot reach this function.
func safeHref(filename string) template.URL {
	return template.URL(fmt.Sprintf("/books/%s", url.PathEscape(filename)))
}
