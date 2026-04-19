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
		Filename, Title, Subtitle string
		Authors                   []string
		Status, SeriesName        string
		SeriesIndex               any
		Rating                    *int
		Cover                     string
		StartedDates              []string
		FinishedDates             []string
	}
	data := struct {
		CSRFToken, RequestID, ActiveNav string
		Books                           []bookRow
		Filter                          struct{ Status, Query string }
	}{
		CSRFToken: "t", RequestID: "r", ActiveNav: "library",
	}
	data.Books = append(data.Books, bookRow{
		Filename: "Hyperion by Dan Simmons.md", Title: "Hyperion",
		Authors: []string{"Dan Simmons"}, Status: "reading", Rating: &rating,
		Cover:        "/covers/" + strings.Repeat("a", 64) + ".jpg",
		StartedDates: []string{"2025-06-01"},
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

func TestBookDetailRatingGridHasFiveAxes(t *testing.T) {
	// Session 15 replaced the single-axis star row with a 5-axis
	// Trial-System grid. Each axis is a nested <fieldset> with a
	// visible legend, a star row, and a "+" bump button.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	axes := []map[string]any{}
	for _, a := range []struct{ key, label string }{
		{"emotional_impact", "Emotional Impact"},
		{"characters", "Characters"},
		{"plot", "Plot"},
		{"dialogue_prose", "Dialogue/Prose"},
		{"cinematography_worldbuilding", "Cinematography/Worldbuilding"},
	} {
		axes = append(axes, map[string]any{
			"Key":           a.key,
			"Label":         a.label,
			"Stars":         []int{1, 2, 3, 4, 5},
			"SelectedValue": 0,
		})
	}
	data := map[string]any{
		"CSRFToken": "t", "RequestID": "r", "ActiveNav": "library",
		"Warnings":       []string{},
		"RatingAxes":     axes,
		"OverallDisplay": "—",
		"OverrideValue":  "",
		"Book": map[string]any{
			"Filename": "Foo by Bar.md", "Title": "Foo",
			"Authors": []string{"Bar"}, "Status": "reading",
			"Rating":        nil,
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
	if !strings.Contains(out, `class="rating-grid"`) {
		t.Errorf("outer rating-grid fieldset missing; body:\n%s", out)
	}
	for _, axisKey := range []string{
		"emotional_impact", "characters", "plot",
		"dialogue_prose", "cinematography_worldbuilding",
	} {
		if !strings.Contains(out, `data-axis="`+axisKey+`"`) {
			t.Errorf("axis %q missing", axisKey)
		}
	}
	if !strings.Contains(out, `class="rating-star"`) {
		t.Errorf("rating-star buttons missing")
	}
	if !strings.Contains(out, `href="#icon-star-filled"`) {
		t.Errorf("star sprite <use> missing")
	}
	if !strings.Contains(out, `data-bump`) {
		t.Errorf("bump button missing")
	}
	if !strings.Contains(out, `data-overall-output`) {
		t.Errorf("aria-live overall output missing")
	}
	if !strings.Contains(out, `data-override-checkbox`) {
		t.Errorf("override checkbox missing")
	}
	if !strings.Contains(out, `data-override-input`) {
		t.Errorf("override input missing")
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

func TestRatingGridUsesFieldsetWithLegend(t *testing.T) {
	// Session 15 preserves the Session 9 fieldset+legend pattern at the
	// outer rating-grid level: the outer fieldset has an sr-only legend
	// ("Trial System rating"), and each of the five axis fieldsets
	// carries a visible legend. The <h2>Rating</h2> outline anchor stays.
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	axes := []map[string]any{}
	for _, a := range []struct{ key, label string }{
		{"emotional_impact", "Emotional Impact"},
		{"characters", "Characters"},
		{"plot", "Plot"},
		{"dialogue_prose", "Dialogue/Prose"},
		{"cinematography_worldbuilding", "Cinematography/Worldbuilding"},
	} {
		axes = append(axes, map[string]any{
			"Key":   a.key,
			"Label": a.label,
			"Stars": []int{1, 2, 3, 4, 5}, "SelectedValue": 0,
		})
	}
	data := map[string]any{
		"CSRFToken":      "t",
		"RequestID":      "r",
		"ActiveNav":      "library",
		"Warnings":       []string{},
		"RatingAxes":     axes,
		"OverallDisplay": "—",
		"OverrideValue":  "",
		"Book": map[string]any{
			"Filename":      "Foo by Bar.md",
			"Title":         "Foo",
			"Authors":       []string{"Bar"},
			"Status":        "reading",
			"Rating":        nil,
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
	if !strings.Contains(out, `<fieldset class="rating-grid"`) {
		t.Errorf("rating grid must be a <fieldset class=\"rating-grid\"; body:\n%s", out)
	}
	if !strings.Contains(out, `<legend class="sr-only">Trial System rating</legend>`) {
		t.Errorf("outer grid missing sr-only <legend>; body:\n%s", out)
	}
	for _, want := range []string{
		`<legend>Emotional Impact</legend>`,
		`<legend>Characters</legend>`,
		`<legend>Plot</legend>`,
		`<legend>Dialogue/Prose</legend>`,
		`<legend>Cinematography/Worldbuilding</legend>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing axis legend %q; body:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `<h2>Rating</h2>`) {
		t.Errorf("book_detail outline must still emit <h2>Rating</h2>; body:\n%s", out)
	}
	// No leftover role="group" on the rating grid itself.
	ratingRx := regexp.MustCompile(`(?is)class="rating-grid"[^>]*role="group"`)
	if ratingRx.MatchString(out) {
		t.Errorf("rating grid should not carry role=\"group\" once <fieldset> is in place; body:\n%s", out)
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

func TestFormatDate(t *testing.T) {
	cases := map[string]string{
		"2025-04-18": "2025-04-18",
		" 2025-04-18 ": "2025-04-18",
		"":            "",
		"not-a-date":  "",
		"2025/04/18":  "",
	}
	for in, want := range cases {
		if got := formatDate(in); got != want {
			t.Errorf("formatDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastDate(t *testing.T) {
	if lastDate(nil) != "" {
		t.Errorf("nil must return empty")
	}
	if lastDate([]string{}) != "" {
		t.Errorf("empty must return empty")
	}
	if lastDate([]string{"a", "b", "c"}) != "c" {
		t.Errorf("last element should win")
	}
}

func TestSearchText(t *testing.T) {
	got := searchText("  The  Ocean ", "At the End of the Lane", "Neverwhere",
		[]string{"Neil Gaiman"})
	want := "the ocean at the end of the lane neverwhere neil gaiman"
	if got != want {
		t.Errorf("searchText = %q, want %q", got, want)
	}
}

func TestDateChipText(t *testing.T) {
	// finished → last finished
	if got := dateChipText("finished", []string{"2025-01-01"}, []string{"2025-03-15", "2025-04-10"}); got != "Finished 2025-04-10" {
		t.Errorf("finished chip = %q", got)
	}
	// reading/paused/dnf → last started
	if got := dateChipText("reading", []string{"2025-02-01"}, nil); got != "Reading since 2025-02-01" {
		t.Errorf("reading chip = %q", got)
	}
	if got := dateChipText("paused", []string{"2025-02-01", "2025-05-01"}, nil); got != "Paused since 2025-05-01" {
		t.Errorf("paused chip = %q", got)
	}
	if got := dateChipText("dnf", []string{"2025-02-01"}, nil); got != "Stopped 2025-02-01" {
		t.Errorf("dnf chip = %q", got)
	}
	// unread → empty
	if got := dateChipText("unread", []string{"2025-02-01"}, nil); got != "" {
		t.Errorf("unread chip = %q, want empty", got)
	}
	// finished without finish date → empty (no fabricated chip)
	if got := dateChipText("finished", []string{"2025-02-01"}, nil); got != "" {
		t.Errorf("finished-without-date chip = %q, want empty", got)
	}
}

// TestLibraryHasSearchInput asserts the /library filter bar carries a
// type="search" input with id="library-search" and role="search" on the
// form; and that the search input appears *before* the status select in
// DOM order so the `/` keyboard shortcut focuses it first via
// `.filter-bar input` selector.
func TestLibraryHasSearchInput(t *testing.T) {
	data, err := fs.ReadFile(FS(), "library.html")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, `role="search"`) {
		t.Errorf("library.html filter-bar must have role=\"search\"")
	}
	if !strings.Contains(src, `id="library-search"`) || !strings.Contains(src, `type="search"`) {
		t.Errorf("library.html must have <input id=\"library-search\" type=\"search\">")
	}
	searchIdx := strings.Index(src, `id="library-search"`)
	statusIdx := strings.Index(src, `id="status-filter"`)
	if searchIdx < 0 || statusIdx < 0 {
		t.Fatalf("expected both search and status inputs; searchIdx=%d statusIdx=%d", searchIdx, statusIdx)
	}
	if searchIdx > statusIdx {
		t.Errorf("#library-search must appear before #status-filter so `/` shortcut focuses search first")
	}
	if !strings.Contains(src, `id="search-empty"`) {
		t.Errorf("library.html must carry a #search-empty region for no-match state")
	}
	if !strings.Contains(src, `aria-live="polite"`) {
		t.Errorf("library.html must carry aria-live on the count region")
	}
}

// TestBookCardHasDataSearchAttr asserts the shared bookCard partial
// renders a data-search attribute (client-side filter haystack) and
// that searchText() produces a lowercase, whitespace-normalized value.
func TestBookCardHasDataSearchAttr(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	row := struct {
		Filename, Title, Subtitle, SeriesName, Status string
		Authors                                       []string
		Rating                                        *int
		SeriesIndex                                   any
		Cover                                         string
		StartedDates, FinishedDates                   []string
	}{
		Filename:   "Dune by Frank Herbert.md",
		Title:      "Dune",
		Subtitle:   "Dune Chronicles 1",
		Authors:    []string{"Frank Herbert"},
		SeriesName: "Dune",
		Status:     "finished",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "bookCard", row); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-search="`) {
		t.Errorf("bookCard missing data-search attr; got: %s", out)
	}
	if !strings.Contains(out, "dune dune chronicles 1 dune frank herbert") {
		t.Errorf("searchText should emit lowercase normalized haystack; got: %s", out)
	}
}

// TestBookDetailRendersStructuredTimeline exercises the paired-entry
// rendering path: two finished entries + one unfinished+paused entry
// should produce three <li>s, each with a <time datetime="…"> element.
// The final entry carries the 'paused' state label.
func TestBookDetailRendersStructuredTimeline(t *testing.T) {
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
			"Filename":      "Cryptonomicon by Neal Stephenson.md",
			"Title":         "Cryptonomicon",
			"Authors":       []string{"Neal Stephenson"},
			"Status":        "paused",
			"Rating":        (*int)(nil),
			"Review":        "",
			"TimelineLines": []string{},
			"CanonicalName": true,
			"Cover":         "",
			"ISBN":          "",
			"SeriesName":    "",
			"SeriesIndex":   (*float64)(nil),
			"StartedDates":  []string{"2024-01-10", "2025-01-15", "2025-11-20"},
			"FinishedDates": []string{"2024-03-20", "2025-03-28"},
			"Timeline": []map[string]any{
				{"Started": "2024-01-10", "Finished": "2024-03-20", "State": "finished", "Label": "Started 2024-01-10, finished 2024-03-20"},
				{"Started": "2025-01-15", "Finished": "2025-03-28", "State": "finished", "Label": "Started 2025-01-15, finished 2025-03-28"},
				{"Started": "2025-11-20", "Finished": "", "State": "paused", "Label": "Paused, last started 2025-11-20"},
			},
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "book_detail", data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="timeline-entries"`) {
		t.Errorf("missing <ol class=\"timeline-entries\">; got:\n%s", out)
	}
	if strings.Count(out, "<li class=\"timeline-entry") != 3 {
		t.Errorf("expected 3 timeline-entry <li>s, got output:\n%s", out)
	}
	if !strings.Contains(out, `<time datetime="2024-03-20">2024-03-20</time>`) {
		t.Errorf("first finished entry must render <time datetime=\"…\">; got:\n%s", out)
	}
	if !strings.Contains(out, `class="timeline-entry timeline-entry--paused"`) {
		t.Errorf("final entry must carry timeline-entry--paused class; got:\n%s", out)
	}
	if !strings.Contains(out, `aria-label="Paused, last started 2025-11-20"`) {
		t.Errorf("final entry must carry composed aria-label; got:\n%s", out)
	}
	// Empty-state should NOT render when Timeline is non-empty.
	if strings.Contains(out, "No timeline entries yet") {
		t.Errorf("empty state must not render when Timeline has entries; got:\n%s", out)
	}
}

// TestCardCarriesDateChipForFinishedBook asserts the finished chip
// renders the last finished date and that unread books render no chip.
func TestCardCarriesDateChipForFinishedBook(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	type card struct {
		Filename, Title, Subtitle, SeriesName, Status string
		Authors                                       []string
		Rating                                        *int
		SeriesIndex                                   any
		Cover                                         string
		StartedDates, FinishedDates                   []string
	}
	finished := card{
		Filename: "A by Z.md", Title: "A", Authors: []string{"Z"},
		Status:        "finished",
		StartedDates:  []string{"2025-02-01"},
		FinishedDates: []string{"2025-04-10"},
	}
	unread := card{
		Filename: "B by Z.md", Title: "B", Authors: []string{"Z"},
		Status: "unread",
	}
	var fbuf, ubuf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&fbuf, "bookCard", finished); err != nil {
		t.Fatalf("finished execute: %v", err)
	}
	if err := tmpl.ExecuteTemplate(&ubuf, "bookCard", unread); err != nil {
		t.Fatalf("unread execute: %v", err)
	}
	if !strings.Contains(fbuf.String(), `class="date-chip"`) {
		t.Errorf("finished card missing date-chip; got:\n%s", fbuf.String())
	}
	if !strings.Contains(fbuf.String(), "Finished 2025-04-10") {
		t.Errorf("finished card must show the last finished date; got:\n%s", fbuf.String())
	}
	if strings.Contains(ubuf.String(), `class="date-chip"`) {
		t.Errorf("unread card must NOT render a date-chip; got:\n%s", ubuf.String())
	}
}

// TestSpriteHasIconAudioSymbol — Session 14 regression guard. The
// book-detail timeline references #icon-audio for entries tagged
// Source=="audiobookshelf"; the sprite must actually define it, at
// 24×24 like every other nav-adjacent icon.
func TestSpriteHasIconAudioSymbol(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "iconSprite", nil); err != nil {
		t.Fatalf("execute iconSprite: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `id="icon-audio"`) {
		t.Errorf("sprite missing icon-audio symbol; body:\n%s", out)
	}
	// icon-audio must share the 24×24 viewBox used by nav-adjacent icons
	// so inline sizing (.icon, .timeline-source-icon) renders correctly.
	// We assert on the full attribute substring to avoid matching the
	// 64×64 empty-state icons.
	if !strings.Contains(out, `id="icon-audio" viewBox="0 0 24 24"`) {
		t.Errorf("icon-audio must share the 24x24 viewBox; body:\n%s", out)
	}
}

// TestSyncPageRendersWhenConfigured asserts the /sync SSR page emits
// the plan-form + plan-output + apply-btn + apply-report surface when
// the Audiobookshelf provider is wired. This is the primary regression
// guard for Session 14's UI scaffold — if any of the id="…" targets
// gets renamed, initSync() in app.js will silently no-op.
func TestSyncPageRendersWhenConfigured(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"CSRFToken":           "t",
		"RequestID":           "r",
		"ActiveNav":           "sync",
		"AudiobookshelfWired": true,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "sync", data); err != nil {
		t.Fatalf("execute sync: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`<form id="plan-form"`,
		`class="sync-plan-form"`,
		`id="plan-output"`,
		`id="apply-btn"`,
		`id="apply-report"`,
		`class="sync-apply-row"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync page missing %q; body:\n%s", want, out)
		}
	}
	// The empty-state copy must NOT render when wired.
	if strings.Contains(out, "is not configured") {
		t.Errorf("configured branch must not render the not-configured banner; body:\n%s", out)
	}
}

// TestSyncPageEmptyStateWhenDisabled asserts the /sync SSR page renders
// only the "not configured" banner when the Audiobookshelf provider is
// nil. The plan-form + apply-btn markup must be absent so the page
// makes the disabled state unambiguous and initSync() in app.js has
// nothing to hook into.
func TestSyncPageEmptyStateWhenDisabled(t *testing.T) {
	tmpl, err := Parse()
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"CSRFToken":           "t",
		"RequestID":           "r",
		"ActiveNav":           "sync",
		"AudiobookshelfWired": false,
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "sync", data); err != nil {
		t.Fatalf("execute sync: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "is not configured") {
		t.Errorf("disabled branch must render the not-configured banner; body:\n%s", out)
	}
	for _, unwanted := range []string{
		`<form id="plan-form"`,
		`id="apply-btn"`,
		`class="sync-plan-form"`,
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("disabled branch must NOT render %q; body:\n%s", unwanted, out)
		}
	}
}

// TestTimelineShowsAudioBadgeOnABEntries — Session 14 regression guard.
// Book-detail must emit #icon-audio + the sr-only "Source:
// Audiobookshelf" label on timeline entries whose Source ==
// "audiobookshelf", and must NOT emit it on vault-origin entries.
func TestTimelineShowsAudioBadgeOnABEntries(t *testing.T) {
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
			"Filename":      "Project Hail Mary by Andy Weir.md",
			"Title":         "Project Hail Mary",
			"Authors":       []string{"Andy Weir"},
			"Status":        "finished",
			"Rating":        (*int)(nil),
			"Review":        "",
			"TimelineLines": []string{},
			"CanonicalName": true,
			"Cover":         "",
			"ISBN":          "",
			"SeriesName":    "",
			"SeriesIndex":   (*float64)(nil),
			"StartedDates":  []string{"2024-05-01", "2025-01-10"},
			"FinishedDates": []string{"2024-06-03", "2025-02-14"},
			"Timeline": []map[string]any{
				{"Started": "2024-05-01", "Finished": "2024-06-03", "State": "finished", "Label": "Started 2024-05-01, finished 2024-06-03", "Source": ""},
				{"Started": "2025-01-10", "Finished": "2025-02-14", "State": "finished", "Label": "Started 2025-01-10, finished 2025-02-14", "Source": "audiobookshelf"},
			},
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "book_detail", data); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	// Exactly one AB entry → exactly one #icon-audio reference and
	// exactly one sr-only label. A count assertion (not just Contains)
	// guards against the badge accidentally firing on vault entries.
	if got := strings.Count(out, `href="#icon-audio"`); got != 1 {
		t.Errorf("expected 1 #icon-audio reference, got %d; body:\n%s", got, out)
	}
	if got := strings.Count(out, "Source: Audiobookshelf"); got != 1 {
		t.Errorf("expected 1 sr-only 'Source: Audiobookshelf' label, got %d; body:\n%s", got, out)
	}
	if !strings.Contains(out, `class="icon timeline-source-icon"`) {
		t.Errorf("audio badge must carry timeline-source-icon class; body:\n%s", out)
	}
}
