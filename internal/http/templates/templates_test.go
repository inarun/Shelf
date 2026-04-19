package templates

import (
	"bytes"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, name := range []string{"library", "book_detail", "import", "error", "head", "nav", "scripts", "bookCard",
		"add", "series_list", "series_detail", "stats", "iconSprite", "helpOverlay"} {
		if tmpl.Lookup(name) == nil {
			t.Errorf("template %q not defined", name)
		}
	}
}

func TestLibraryRendersBooks(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	rating := 4
	type bookRow struct {
		Filename, Title    string
		Authors            []string
		Status, SeriesName string
		SeriesIndex        any
		Rating             *int
		Cover              string
	}
	data := struct {
		CSRFToken, RequestID, ActiveNav string
		Books                           []bookRow
		Filter                          struct{ Status string }
	}{
		CSRFToken: "t", RequestID: "r", ActiveNav: "library",
	}
	data.Books = append(data.Books, bookRow{
		Filename: "Hyperion by Dan Simmons.md", Title: "Hyperion",
		Authors: []string{"Dan Simmons"}, Status: "reading", Rating: &rating,
		Cover: "/covers/" + strings.Repeat("a", 64) + ".jpg",
	})

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "library", data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Hyperion") {
		t.Errorf("missing title; got:\n%s", out)
	}
	if !strings.Contains(out, "Dan Simmons") {
		t.Errorf("missing author; got:\n%s", out)
	}
	if !strings.Contains(out, `<meta name="csrf-token" content="t">`) {
		t.Errorf("missing CSRF meta; got:\n%s", out)
	}
}

func TestHTMLEscapesUserContent(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate book_detail with review containing a <script> attempt.
	data := map[string]any{
		"CSRFToken":  "t",
		"RequestID":  "r",
		"ActiveNav":  "library",
		"Warnings":   []string{},
		"RatingRange": []int{1, 2, 3, 4, 5},
		"Book": map[string]any{
			"Filename":      "Foo by Bar.md",
			"Title":         "Foo",
			"Authors":       []string{"Bar"},
			"Status":        "reading",
			"Rating":        (*int)(nil),
			"Review":        `<script>alert('x')</script>`,
			"TimelineLines": []string{},
			"CanonicalName": true,
			"Cover":         "",
			"ISBN":          "",
			"SeriesName":    "",
			"SeriesIndex":   (*float64)(nil),
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "book_detail", data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script>alert('x')</script>") {
		t.Errorf("review text must be escaped; got raw script in output")
	}
	if !strings.Contains(out, "&lt;script&gt;") && !strings.Contains(out, "&#34;") {
		t.Errorf("expected escaped content; got:\n%s", out)
	}
}

func TestNoInlineStyleAttributesInTemplates(t *testing.T) {
	// Strict CSP style-src 'self' (see internal/http/middleware/csp.go)
	// rejects any element carrying a raw style="" attribute. Keep all
	// styling in app.css + named classes; template helpers like
	// barWidthClass emit class tokens, never style strings.
	styleAttr := regexp.MustCompile(`(?is)<[^>]*\sstyle\s*=`)
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".html" {
			return nil
		}
		data, err := fs.ReadFile(FS(), p)
		if err != nil {
			return err
		}
		for _, match := range styleAttr.FindAllString(string(data), -1) {
			t.Errorf("inline style attribute in %s — blocked by CSP style-src 'self': %s",
				p, strings.TrimSpace(match))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStatsRenderUsesClassNotInlineStyle(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"CSRFToken": "t", "RequestID": "r", "ActiveNav": "stats",
		"Summary": map[string]any{
			"TotalBooks":    3,
			"TotalReads":    5,
			"RatedBooks":    2,
			"AverageRating": 4.5,
			"StatusCounts": map[string]int{
				"unread": 1, "reading": 1, "finished": 1, "paused": 0, "dnf": 0,
			},
		},
		"OrderedStatus": []string{"unread", "reading", "finished", "paused", "dnf"},
		"Years": []map[string]any{
			{"Year": 2024, "Books": int64(2), "Pages": int64(600)},
			{"Year": 2025, "Books": int64(3), "Pages": int64(900)},
		},
		"MaxYearBooks": int64(3),
		"MaxYearPages": int64(900),
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "stats", data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `style=`) {
		t.Errorf("stats template must not emit inline style= (CSP violation); got:\n%s", out)
	}
	if !strings.Contains(out, `bar--w`) {
		t.Errorf("stats bars must carry a bar--wN class; got:\n%s", out)
	}
}

func TestNavEmitsIconSpriteAndHelpOverlay(t *testing.T) {
	// Every page goes through {{template "nav" .}}, which in Session 8
	// pulls in the inline SVG sprite + the keyboard-shortcut help overlay.
	// If either drops out, in-page <use href="#icon-..."/> references go
	// dead and the ? shortcut has nothing to show.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := map[string]any{
		"CSRFToken": "t", "RequestID": "r", "ActiveNav": "library",
	}
	if err := tmpl.ExecuteTemplate(&buf, "nav", data); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`id="icon-star-filled"`,
		`id="icon-keyboard"`,
		`id="icon-x"`,
		`id="kbd-help"`,
		`id="kbd-help-btn"`,
		`data-kbd-help-dismiss`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("nav output missing %q; full body:\n%s", want, out)
		}
	}
}

func TestBookDetailRatingUsesStarIcons(t *testing.T) {
	// Session 8 replaced the numeric rating buttons with star SVG buttons.
	// The <use href="#icon-star-filled"/> reference ties each button to the
	// sprite symbol; if this test fails, either the sprite id changed or
	// the template regressed.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"CSRFToken": "t", "RequestID": "r", "ActiveNav": "library",
		"Warnings":    []string{},
		"RatingRange": []int{1, 2, 3, 4, 5},
		"Book": map[string]any{
			"Filename": "Foo by Bar.md", "Title": "Foo",
			"Authors": []string{"Bar"}, "Status": "reading",
			"Rating":        (*int)(nil),
			"Review":        "",
			"TimelineLines": []string{},
			"CanonicalName": true,
			"Cover":         "", "ISBN": "",
			"SeriesName":  "",
			"SeriesIndex": (*float64)(nil),
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "book_detail", data); err != nil {
		t.Fatalf("execute book_detail: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="rating-star"`) {
		t.Errorf("rating buttons missing rating-star class")
	}
	if !strings.Contains(out, `href="#icon-star-filled"`) {
		t.Errorf("rating buttons missing star SVG <use>; body:\n%s", out)
	}
	if !strings.Contains(out, `aria-label="1 star"`) || !strings.Contains(out, `aria-label="5 stars"`) {
		t.Errorf("rating buttons missing pluralized aria-label; body:\n%s", out)
	}
}

func TestSkipLinkOnEveryPage(t *testing.T) {
	// Every top-level page must carry id="main" on its <main> so the
	// skip-to-content link resolves; the shared nav partial must emit
	// the link itself. Partials (underscore-prefixed) are skipped.
	mainRx := regexp.MustCompile(`(?is)<main\b[^>]*>`)
	mainIDRx := regexp.MustCompile(`(?is)<main\b[^>]*\bid\s*=\s*"main"`)
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".html" {
			return nil
		}
		if strings.HasPrefix(path.Base(p), "_") {
			return nil
		}
		data, err := fs.ReadFile(FS(), p)
		if err != nil {
			return err
		}
		body := string(data)
		if mainRx.MatchString(body) && !mainIDRx.MatchString(body) {
			t.Errorf("%s has <main> but is missing id=\"main\" — skip-link anchor will not resolve", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := map[string]any{"CSRFToken": "t", "RequestID": "r", "ActiveNav": "library"}
	if err := tmpl.ExecuteTemplate(&buf, "nav", data); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`class="skip-link"`, `href="#main"`, `Skip to main content`} {
		if !strings.Contains(out, want) {
			t.Errorf("nav missing %q; body:\n%s", want, out)
		}
	}
}

func TestRatingUsesFieldsetWithLegend(t *testing.T) {
	// Session 9 upgraded the rating widget to <fieldset><legend>...
	// The <h2>Rating</h2> heading stays for outline; the legend is
	// sr-only. The old role="group" + aria-label="Star rating" are
	// removed because the fieldset+legend covers both.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"CSRFToken":   "t",
		"RequestID":   "r",
		"ActiveNav":   "library",
		"Warnings":    []string{},
		"RatingRange": []int{1, 2, 3, 4, 5},
		"Book": map[string]any{
			"Filename":      "Foo by Bar.md",
			"Title":         "Foo",
			"Authors":       []string{"Bar"},
			"Status":        "reading",
			"Rating":        (*int)(nil),
			"Review":        "",
			"TimelineLines": []string{},
			"CanonicalName": true,
			"Cover":         "", "ISBN": "",
			"SeriesName":  "",
			"SeriesIndex": (*float64)(nil),
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "book_detail", data); err != nil {
		t.Fatalf("execute book_detail: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `<fieldset class="rating-widget"`) {
		t.Errorf("rating widget must be a <fieldset class=\"rating-widget\"; body:\n%s", out)
	}
	if !strings.Contains(out, `<legend class="sr-only">Star rating</legend>`) {
		t.Errorf("rating widget missing sr-only <legend>; body:\n%s", out)
	}
	// The <h2>Rating</h2> outline anchor stays.
	if !strings.Contains(out, `<h2>Rating</h2>`) {
		t.Errorf("book_detail outline must still emit <h2>Rating</h2>; body:\n%s", out)
	}
	// No leftover role="group" on the rating widget itself.
	ratingRx := regexp.MustCompile(`(?is)class="rating-widget"[^>]*role="group"`)
	if ratingRx.MatchString(out) {
		t.Errorf("rating widget should not carry role=\"group\" once <fieldset> is in place; body:\n%s", out)
	}
}

func TestEmptyStatesRenderIllustration(t *testing.T) {
	// Every zero-data page branch must render the illustrated empty-state
	// component (container + icon <use>), not a bare paragraph. Using
	// table-driven data lets a new empty state plug in without
	// restructuring the test.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"CSRFToken": "t", "RequestID": "r"}

	seriesSummary := map[string]any{
		"StatusCounts":  map[string]int{"unread": 0, "reading": 0, "finished": 0, "paused": 0, "dnf": 0},
		"TotalBooks":    0,
		"TotalReads":    0,
		"RatedBooks":    0,
		"AverageRating": 0.0,
	}

	cases := []struct {
		tmplName string
		data     map[string]any
		iconID   string
	}{
		{
			tmplName: "library",
			data: mergeMap(base, map[string]any{
				"ActiveNav": "library",
				"Books":     []any{},
				"Filter":    map[string]string{"Status": ""},
			}),
			iconID: "icon-empty-shelf",
		},
		{
			tmplName: "series_list",
			data: mergeMap(base, map[string]any{
				"ActiveNav": "series",
				"Series":    []any{},
			}),
			iconID: "icon-empty-shelf",
		},
		{
			tmplName: "series_detail",
			data: mergeMap(base, map[string]any{
				"ActiveNav": "series",
				"Series": map[string]any{
					"Name":      "Mistborn",
					"Finished":  0,
					"BookCount": 0,
					"Books":     []any{},
				},
			}),
			iconID: "icon-empty-shelf",
		},
		{
			tmplName: "stats",
			data: mergeMap(base, map[string]any{
				"ActiveNav":     "stats",
				"Summary":       seriesSummary,
				"OrderedStatus": []string{"unread", "reading", "finished", "paused", "dnf"},
				"Years":         []map[string]any{},
				"MaxYearBooks":  int64(0),
				"MaxYearPages":  int64(0),
			}),
			iconID: "icon-empty-chart",
		},
		{
			tmplName: "book_detail",
			data: mergeMap(base, map[string]any{
				"ActiveNav":   "library",
				"Warnings":    []string{},
				"RatingRange": []int{1, 2, 3, 4, 5},
				"Book": map[string]any{
					"Filename":      "Foo by Bar.md",
					"Title":         "Foo",
					"Authors":       []string{"Bar"},
					"Status":        "unread",
					"Rating":        (*int)(nil),
					"Review":        "",
					"TimelineLines": []string{},
					"CanonicalName": true,
					"Cover":         "", "ISBN": "",
					"SeriesName":  "",
					"SeriesIndex": (*float64)(nil),
				},
			}),
			iconID: "icon-empty-timeline",
		},
	}

	for _, tc := range cases {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, tc.tmplName, tc.data); err != nil {
			t.Errorf("execute %s: %v", tc.tmplName, err)
			continue
		}
		out := buf.String()
		if !strings.Contains(out, `class="empty-state__icon"`) {
			t.Errorf("%s: empty-state component missing illustration container; body:\n%s", tc.tmplName, out)
		}
		if !strings.Contains(out, `href="#`+tc.iconID+`"`) {
			t.Errorf("%s: expected sprite reference #%s; body:\n%s", tc.tmplName, tc.iconID, out)
		}
	}
}

func TestStatsBarsCarryWidthClass(t *testing.T) {
	// The Session 9 bar-chart entry animation (initBarAnimation in app.js)
	// pivots on the server-rendered .bar--wN class. Without it the JS
	// would have no target to swap to and reduced-motion / no-JS users
	// would see 0%-width bars. This guard keeps the render invariant.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"CSRFToken": "t", "RequestID": "r", "ActiveNav": "stats",
		"Summary": map[string]any{
			"TotalBooks": 1, "TotalReads": 1, "RatedBooks": 1, "AverageRating": 5.0,
			"StatusCounts": map[string]int{"unread": 0, "reading": 0, "finished": 1, "paused": 0, "dnf": 0},
		},
		"OrderedStatus": []string{"unread", "reading", "finished", "paused", "dnf"},
		"Years":         []map[string]any{{"Year": 2025, "Books": int64(1), "Pages": int64(300)}},
		"MaxYearBooks":  int64(1),
		"MaxYearPages":  int64(300),
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "stats", data); err != nil {
		t.Fatalf("execute stats: %v", err)
	}
	out := buf.String()
	barRx := regexp.MustCompile(`(?is)<span class="bar[^"]*\bbar--w\d+\b[^"]*"`)
	if !barRx.MatchString(out) {
		t.Errorf("every .bar must carry a bar--wN class; body:\n%s", out)
	}
}

func TestFormsUseExplicitLabelAssociation(t *testing.T) {
	// Every form control across library / add / import should be paired
	// with a <label for="id"> that matches the control's id. Matches
	// the Session 9 label-density cleanup.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	type page struct {
		name string
		data map[string]any
	}
	pages := []page{
		{"library", map[string]any{
			"CSRFToken": "t", "RequestID": "r", "ActiveNav": "library",
			"Books": []any{}, "Filter": map[string]string{"Status": ""},
		}},
		{"add", map[string]any{
			"CSRFToken": "t", "RequestID": "r", "ActiveNav": "add",
			"ProviderWired": true,
		}},
		{"import", map[string]any{
			"CSRFToken": "t", "RequestID": "r", "ActiveNav": "import",
		}},
	}
	idRx := regexp.MustCompile(`(?is)<(?:input|select|textarea)\b[^>]*\bid\s*=\s*"([^"]+)"`)
	for _, pg := range pages {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, pg.name, pg.data); err != nil {
			t.Errorf("execute %s: %v", pg.name, err)
			continue
		}
		out := buf.String()
		ids := idRx.FindAllStringSubmatch(out, -1)
		if len(ids) == 0 {
			t.Errorf("%s: expected at least one form control with an id", pg.name)
			continue
		}
		for _, m := range ids {
			id := m[1]
			want := `for="` + id + `"`
			if !strings.Contains(out, want) {
				t.Errorf("%s: form control id=%q has no matching <label for=%q>; body:\n%s",
					pg.name, id, id, out)
			}
		}
	}
}

// mergeMap is a tiny helper for table-driven tests — composes a base
// data map with per-case overrides without mutating the base.
func mergeMap(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestNavBrandUsesLogoMarkAndWordmark(t *testing.T) {
	// Session 10 replaced the plain-text "Shelf" brand link with a logo
	// mark + wordmark pair. The link keeps its href="/library" and gains
	// aria-label="Shelf — home" so screen readers still announce it as
	// a home link (the visible text is the wordmark span).
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := map[string]any{"CSRFToken": "t", "RequestID": "r", "ActiveNav": "library"}
	if err := tmpl.ExecuteTemplate(&buf, "nav", data); err != nil {
		t.Fatalf("execute nav: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`class="brand"`,
		`href="/library"`,
		// html/template leaves non-ASCII unescaped in attribute context,
		// so the em-dash passes through verbatim.
		`aria-label="Shelf — home"`,
		`class="brand-mark"`,
		`href="#icon-logo"`,
		`class="brand-wordmark"`,
		`>Shelf</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("nav brand missing %q; body:\n%s", want, out)
		}
	}
}

func TestSpriteHasIconLogoSymbol(t *testing.T) {
	// The nav brand references #icon-logo; the sprite must actually
	// define it. If this guard trips, brand-mark will render blank.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "iconSprite", nil); err != nil {
		t.Fatalf("execute iconSprite: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `id="icon-logo"`) {
		t.Errorf("sprite missing icon-logo symbol; body:\n%s", out)
	}
	// Sanity: the symbol should be 24×24 so the existing .icon sizing
	// works with it (1.25rem → ~20px at 15px base).
	if !strings.Contains(out, `viewBox="0 0 24 24"`) {
		t.Errorf("icon-logo should share the 24x24 viewBox used by other nav icons; body:\n%s", out)
	}
}

func TestNoInlineScriptsInTemplates(t *testing.T) {
	// Every <script> tag must have a src= attribute. CSP default-src 'self'
	// disallows inline scripts; an external self-hosted reference is fine.
	scriptOpen := regexp.MustCompile(`(?is)<script\b[^>]*>`)
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".html" {
			return nil
		}
		data, err := fs.ReadFile(FS(), p)
		if err != nil {
			return err
		}
		for _, match := range scriptOpen.FindAllString(string(data), -1) {
			if !strings.Contains(strings.ToLower(match), "src=") {
				t.Errorf("inline (src-less) script found in %s: %s", p, match)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
