//go:build ignore

// Command a11y-check verifies Shelf's design-token color contrast ratios
// against WCAG 2.2 AA. It parses CSS custom properties (`--*: #rrggbb;`)
// out of the :root and @media (prefers-color-scheme: dark) :root blocks
// in internal/http/static/app.css, then:
//
//  1. Checks a curated list of (foreground, background) pairs that
//     actually ship in the UI. Any AA failure exits non-zero.
//  2. Prints an informational "full matrix" audit of every remaining
//     foreground-looking token against every background-looking token.
//     Honors the SKILL.md wording "every (fg, bg) pair" without
//     drowning the signal in nonsense pairings.
//
// Both the light and dark palettes are checked. Stdlib-only; //go:build
// ignore keeps the file out of `go build ./...`, `go test ./...`, and
// the security-lint suite (matches cmd/gen-icons).
//
// Run from the repo root:
//
//	go run cmd/a11y-check/main.go
//
// Optionally pass a single argument pointing at a different CSS file.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// token is a resolved color declaration (sRGB, 0..1 per channel).
// Non-hex values (color-mix(), rgb(), named) are skipped at parse time.
type token struct {
	name    string
	hex     string
	r, g, b float64
}

// palette is a named set of resolved color tokens.
type palette struct {
	name   string
	tokens map[string]token
}

// pair describes a blocking contrast check.
type pair struct {
	fg, bg string
	kind   string
	need   float64
}

// WCAG 2.2 AA thresholds: 4.5 normal text, 3.0 large text / non-text UI.
var blockingPairs = []pair{
	// Body text on every surface.
	{"fg", "bg", "text", 4.5},
	{"fg", "surface", "text", 4.5},
	{"fg", "surface-elev", "text", 4.5},
	// Secondary text.
	{"fg-subtle", "bg", "text", 4.5},
	{"fg-subtle", "surface", "text", 4.5},
	// Tertiary / muted text — historically the tightest pair.
	{"muted", "bg", "text", 4.5},
	{"muted", "surface", "text", 4.5},
	// Accent link text + primary button fill.
	{"accent", "bg", "text", 4.5},
	{"accent", "surface", "text", 4.5},
	{"accent-fg", "accent", "text", 4.5},
	// Status pills (rendered on tinted mixes of --bg, so --bg is the
	// strictest baseline).
	{"success", "bg", "large", 3.0},
	{"warn", "bg", "large", 3.0},
	{"danger", "bg", "large", 3.0},
	// Filled star rating icon — lives on --surface (book cards +
	// book-detail) and --bg (empty-state illustrations). Both must pass
	// the WCAG 2.2 non-text 3:1 threshold.
	{"star", "bg", "ui", 3.0},
	{"star", "surface", "ui", 3.0},
	// --border-strong is intentionally a soft divider token: it is
	// used only for hover states on buttons/inputs (inactive-state
	// WCAG exception) and the keyboard-shortcut <kbd> border on
	// --surface-elev. It is *not* a standalone UI component colour,
	// so it is not gated here.
}

// Classification for the full-matrix audit. Tokens outside these lists
// (spacing, radius, motion, typography) are ignored automatically because
// they never parse as hex colors.
var fgMatrix = []string{
	"fg", "fg-subtle", "muted",
	"accent", "accent-hover", "accent-fg",
	"success", "warn", "danger", "star",
	"border", "border-strong",
}
var bgMatrix = []string{"bg", "surface", "surface-elev", "accent"}

var (
	darkBlock = regexp.MustCompile(`(?s)@media\s*\(\s*prefers-color-scheme:\s*dark\s*\)\s*\{\s*:root\s*\{([^}]+)\}`)
	rootBlock = regexp.MustCompile(`(?s):root\s*\{([^}]+)\}`)
	decl      = regexp.MustCompile(`--([a-zA-Z0-9_-]+)\s*:\s*([^;]+);`)
	hex6      = regexp.MustCompile(`^#([0-9a-fA-F]{6})$`)
	hex3      = regexp.MustCompile(`^#([0-9a-fA-F]{3})$`)
)

func main() {
	cssPath := filepath.Join("internal", "http", "static", "app.css")
	if len(os.Args) > 1 {
		cssPath = os.Args[1]
	}
	data, err := os.ReadFile(cssPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", cssPath, err)
		os.Exit(2)
	}

	light, dark, err := parsePalettes(string(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	fmt.Printf("Shelf a11y-check — WCAG 2.2 AA contrast audit of %s\n", cssPath)

	fails := 0
	for _, p := range []palette{light, dark} {
		fmt.Printf("\n=== %s palette (%d tokens) ===\n", p.name, len(p.tokens))
		fails += runBlocking(p)
		runAudit(p)
	}

	if fails > 0 {
		fmt.Fprintf(os.Stderr, "\n%d blocking contrast failure(s) — see above.\n", fails)
		os.Exit(1)
	}
	fmt.Println("\nAll blocking contrast pairs pass WCAG 2.2 AA.")
}

func parsePalettes(css string) (palette, palette, error) {
	light := palette{name: "light", tokens: map[string]token{}}
	dark := palette{name: "dark", tokens: map[string]token{}}

	darkMatch := darkBlock.FindStringSubmatch(css)
	var darkBody string
	if darkMatch != nil {
		darkBody = darkMatch[1]
		// Remove the entire dark @media block so the :root regex below
		// lands on the light block (first remaining :root in the file).
		css = strings.Replace(css, darkMatch[0], "", 1)
	}

	lightMatch := rootBlock.FindStringSubmatch(css)
	if lightMatch == nil {
		return light, dark, fmt.Errorf("no :root block found for the light palette")
	}
	ingestBody(lightMatch[1], light.tokens)

	if darkBody != "" {
		ingestBody(darkBody, dark.tokens)
	}
	return light, dark, nil
}

func ingestBody(body string, into map[string]token) {
	for _, m := range decl.FindAllStringSubmatch(body, -1) {
		name := m[1]
		val := strings.TrimSpace(m[2])
		t, ok := parseColor(name, val)
		if !ok {
			continue
		}
		into[name] = t
	}
}

func parseColor(name, val string) (token, bool) {
	if m := hex6.FindStringSubmatch(val); m != nil {
		hx := "#" + strings.ToLower(m[1])
		return token{
			name: name,
			hex:  hx,
			r:    byteToF(hx[1:3]),
			g:    byteToF(hx[3:5]),
			b:    byteToF(hx[5:7]),
		}, true
	}
	if m := hex3.FindStringSubmatch(val); m != nil {
		raw := strings.ToLower(m[1])
		hx := "#" + string([]byte{raw[0], raw[0], raw[1], raw[1], raw[2], raw[2]})
		return token{
			name: name,
			hex:  hx,
			r:    byteToF(hx[1:3]),
			g:    byteToF(hx[3:5]),
			b:    byteToF(hx[5:7]),
		}, true
	}
	return token{}, false
}

func byteToF(h string) float64 {
	v, err := strconv.ParseUint(h, 16, 8)
	if err != nil {
		return 0
	}
	return float64(v) / 255.0
}

// linearize converts an sRGB channel (0..1) to its linear-light equivalent
// per the WCAG 2.x formula.
func linearize(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func relativeLuminance(t token) float64 {
	return 0.2126*linearize(t.r) + 0.7152*linearize(t.g) + 0.0722*linearize(t.b)
}

func contrast(a, b token) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	hi, lo := la, lb
	if lb > la {
		hi, lo = lb, la
	}
	return (hi + 0.05) / (lo + 0.05)
}

func runBlocking(p palette) int {
	fmt.Println("\n  Blocking — pairs that ship in the UI:")
	fmt.Printf("    %-16s %-16s %-6s %-7s %s\n", "foreground", "background", "kind", "ratio", "result")
	fmt.Println("    " + strings.Repeat("-", 66))
	fails := 0
	for _, pr := range blockingPairs {
		fg, fok := p.tokens[pr.fg]
		bg, bok := p.tokens[pr.bg]
		if !fok || !bok {
			fmt.Printf("    %-16s %-16s %-6s %-7s MISSING TOKEN\n", pr.fg, pr.bg, pr.kind, "—")
			fails++
			continue
		}
		ratio := contrast(fg, bg)
		result := "PASS"
		if ratio+1e-6 < pr.need {
			result = fmt.Sprintf("FAIL (need >=%.1f)", pr.need)
			fails++
		}
		fmt.Printf("    %-16s %-16s %-6s %-7s %s\n", pr.fg, pr.bg, pr.kind, fmt.Sprintf("%.2f", ratio), result)
	}
	return fails
}

func runAudit(p palette) {
	type row struct {
		fg, bg string
		ratio  float64
	}
	var rows []row
	for _, fgName := range fgMatrix {
		for _, bgName := range bgMatrix {
			if fgName == bgName {
				continue
			}
			fg, fok := p.tokens[fgName]
			bg, bok := p.tokens[bgName]
			if !fok || !bok {
				continue
			}
			rows = append(rows, row{fgName, bgName, contrast(fg, bg)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ratio < rows[j].ratio })

	fmt.Println("\n  Audit (full matrix, informational):")
	fmt.Printf("    %-16s %-16s %s\n", "foreground", "background", "ratio")
	fmt.Println("    " + strings.Repeat("-", 44))
	for _, r := range rows {
		fmt.Printf("    %-16s %-16s %.2f\n", r.fg, r.bg, r.ratio)
	}
}
