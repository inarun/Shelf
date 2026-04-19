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
	"time"
)

//go:embed *.html
var files embed.FS

// FuncMap is exposed so tests can assert the registered helpers.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"join":           func(sep string, a []string) string { return strings.Join(a, sep) },
		"stars":          stars,
		"bumpedBadge":    bumpedBadge,
		"formatRating":   formatRating,
		"statusClass":    statusClass,
		"statusLabel":    statusLabel,
		"safeHref":       safeHref,
		"safeSeriesHref": safeSeriesHref,
		"barWidth":       barWidth,
		"barWidthClass":  barWidthClass,
		"pluralS": func(n int) string {
			if n == 1 {
				return ""
			}
			return "s"
		},
		"emptyOr": func(s, fallback string) string {
			if s == "" {
				return fallback
			}
			return s
		},
		"deref":        derefInt,
		"formatDate":   formatDate,
		"lastDate":     lastDate,
		"searchText":   searchText,
		"dateChipText": dateChipText,
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

// stars renders a rating as five SVG stars drawn from the sprite
// (#icon-star-filled, #icon-star-half, #icon-star-outline). Values
// exceeding 5 are capped at 5 full stars so the card's star row stays
// five-wide; the bumped N/5 chip next to it (see bumpedBadge) conveys
// the overflow. Fractional values with a remainder ≥ 0.5 render a
// half-star sprite; < 0.5 rounds down.
func stars(r any) template.HTML {
	val, ok := ratingFloat(r)
	if !ok {
		return template.HTML(`<span class="rating-empty" aria-hidden="true">—</span>`)
	}
	if val <= 0 {
		return template.HTML(`<span class="rating-empty" aria-hidden="true">—</span>`)
	}
	display := val
	if display > 5 {
		display = 5
	}
	full := int(display)
	half := 0
	if display-float64(full) >= 0.5 {
		half = 1
	}
	if full+half > 5 {
		full = 5 - half
	}
	empty := 5 - full - half
	var b strings.Builder
	for i := 0; i < full; i++ {
		b.WriteString(`<svg class="rating-star" aria-hidden="true" focusable="false"><use href="#icon-star-filled"/></svg>`)
	}
	for i := 0; i < half; i++ {
		b.WriteString(`<svg class="rating-star" aria-hidden="true" focusable="false"><use href="#icon-star-half"/></svg>`)
	}
	for i := 0; i < empty; i++ {
		b.WriteString(`<svg class="rating-star" aria-hidden="true" focusable="false"><use href="#icon-star-outline"/></svg>`)
	}
	// #nosec G203 -- b contains only compile-time string literals chosen
	// by integer counts of full/half/empty stars; no caller-controlled
	// text flows into it, so template.HTML is safe.
	return template.HTML(b.String())
}

// bumpedBadge renders a small "N/5" chip when the rating exceeds 5.
// Returns an empty HTML string otherwise so templates can call it
// unconditionally next to stars().
func bumpedBadge(r any) template.HTML {
	val, ok := ratingFloat(r)
	if !ok || val <= 5 {
		return template.HTML("")
	}
	label := formatRating(val)
	// #nosec G203 -- `label` is produced by formatRating, which emits
	// only a canonicalised numeric string ("4", "4.5", "6") from a
	// float64. No caller-controlled text reaches this template.HTML cast.
	return template.HTML(fmt.Sprintf(
		`<span class="bumped-badge" aria-label="bumped rating %s of 5">%s/5</span>`,
		label, label,
	))
}

// formatRating renders a *float64/float64/*int64/int64 as a short numeric
// string — integers trim the fractional zero ("4" not "4.0") and halves
// keep one decimal ("4.5"). Returns "" when the value is missing.
func formatRating(r any) string {
	val, ok := ratingFloat(r)
	if !ok {
		return ""
	}
	if val == float64(int64(val)) {
		return fmt.Sprintf("%d", int64(val))
	}
	s := fmt.Sprintf("%.1f", val)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// ratingFloat is the float-accepting counterpart of the pre-S16
// ratingInt. The new rating pipeline stores a *float64 (can exceed 5
// for bumped entries) but tests and legacy code paths may still pass
// *int / *int64; accept both for compatibility.
func ratingFloat(r any) (float64, bool) {
	switch v := r.(type) {
	case float64:
		return v, true
	case *float64:
		if v == nil {
			return 0, false
		}
		return *v, true
	case float32:
		return float64(v), true
	case *float32:
		if v == nil {
			return 0, false
		}
		return float64(*v), true
	case int:
		return float64(v), true
	case *int:
		if v == nil {
			return 0, false
		}
		return float64(*v), true
	case int64:
		return float64(v), true
	case *int64:
		if v == nil {
			return 0, false
		}
		return float64(*v), true
	}
	return 0, false
}

func derefInt(v any) int {
	f, ok := ratingFloat(v)
	if !ok {
		return 0
	}
	return int(f)
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

// safeSeriesHref is the sibling of safeHref for series names. Series
// names come from the series table, itself populated from vault
// frontmatter; no user-controlled query string or scheme ever reaches
// this function.
//
// #nosec G203 -- URL-path-escape of a validated identifier; see safeHref.
func safeSeriesHref(name string) template.URL {
	return template.URL(url.PathEscape(name))
}

// barWidth returns a CSS width percentage for bar-chart rows on the
// stats page. max of 0 yields "0%"; values above max are clamped to
// 100%. The returned template.CSS bypasses html/template auto-escaping
// for style attribute values.
//
// Deprecated: retained for legacy callers only. New templates must use
// barWidthClass — inline style="" attributes are blocked by the strict
// style-src 'self' CSP (see §Anti-patterns in SKILL.md).
//
// #nosec G203 -- The output is an integer percentage composed in-package;
// no caller-controlled text flows into it.
func barWidth(value, max int64) template.CSS {
	if max <= 0 || value <= 0 {
		return template.CSS("0%")
	}
	pct := int64(100)
	if value < max {
		pct = value * 100 / max
	}
	return template.CSS(fmt.Sprintf("%d%%", pct))
}

// barWidthClass returns a CSS utility class for bar-chart rows on the
// stats page, discretized to 5% steps (bar--w0, bar--w5, …, bar--w100).
// This replaces barWidth's inline style="" output, which the strict
// style-src 'self' CSP blocks. max <= 0 yields "bar--w0"; values >= max
// yield "bar--w100".
func barWidthClass(value, max int64) string {
	if max <= 0 || value <= 0 {
		return "bar--w0"
	}
	// 0..20 steps of 5%. +half to round to nearest.
	step := (value*20 + max/2) / max
	if step > 20 {
		step = 20
	}
	if step < 0 {
		step = 0
	}
	return fmt.Sprintf("bar--w%d", step*5)
}

// isoDateFormat is the canonical vault date layout. Kept here rather
// than importing from internal/vault/frontmatter so the templates
// package stays a leaf with zero internal dependencies.
const isoDateFormat = "2006-01-02"

// formatDate returns a trimmed ISO date unchanged, or "" for unparseable
// input. The point is to let templates gate rendering on {{if
// (formatDate .)}} without catching malformed cells. Output is the same
// string on success — no locale-aware reformatting here; the UI shows
// canonical YYYY-MM-DD so re-reads sort lexicographically.
func formatDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := time.Parse(isoDateFormat, s); err != nil {
		return ""
	}
	return s
}

// lastDate returns the final element of dates or "" when dates is
// empty. Used to surface the most-recent entry from the started/
// finished arrays.
func lastDate(dates []string) string {
	if len(dates) == 0 {
		return ""
	}
	return dates[len(dates)-1]
}

// searchText composes the lowercased haystack emitted as data-search on
// each book card. The client-side library filter does a plain substring
// check against this string, so all searchable fields must be present
// here. Authors come in as []string (join-separated); everything else
// is a scalar. Whitespace is normalized to single spaces so tests can
// assert exact output.
func searchText(title, subtitle, seriesName string, authors []string) string {
	parts := []string{title, subtitle, seriesName}
	parts = append(parts, authors...)
	joined := strings.Join(parts, " ")
	joined = strings.ToLower(joined)
	return strings.Join(strings.Fields(joined), " ")
}

// dateChipText returns the single-line card chip describing the most
// recent activity for a book, or "" when no chip should render.
// Unread books and books with no dates return ""; templates gate on
// {{with dateChipText …}} to omit the element entirely.
//
// Rules:
//
//	finished → "Finished <last finished date>"
//	reading  → "Reading since <last started date>"
//	paused   → "Paused since <last started date>"
//	dnf      → "Stopped <last started date>"
//	else     → ""
//
// Missing dates fall through to "" — the card still conveys state via
// the existing status pill; the chip adds no extra noise.
func dateChipText(status string, started, finished []string) string {
	switch status {
	case "finished":
		if d := lastDate(finished); d != "" {
			return "Finished " + d
		}
	case "reading":
		if d := lastDate(started); d != "" {
			return "Reading since " + d
		}
	case "paused":
		if d := lastDate(started); d != "" {
			return "Paused since " + d
		}
	case "dnf":
		if d := lastDate(started); d != "" {
			return "Stopped " + d
		}
	}
	return ""
}
