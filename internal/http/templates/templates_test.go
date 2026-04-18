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
