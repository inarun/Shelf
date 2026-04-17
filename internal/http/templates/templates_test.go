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
	for _, name := range []string{"library", "book_detail", "import", "error", "head", "nav", "scripts", "bookCard"} {
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
	data := struct {
		CSRFToken, RequestID, ActiveNav string
		Books                           []struct {
			Filename, Title                string
			Authors                        []string
			Status, SeriesName             string
			SeriesIndex                    any
			Rating                         *int
		}
		Filter struct{ Status string }
	}{
		CSRFToken: "t", RequestID: "r", ActiveNav: "library",
	}
	data.Books = append(data.Books, struct {
		Filename, Title    string
		Authors            []string
		Status, SeriesName string
		SeriesIndex        any
		Rating             *int
	}{
		Filename: "Hyperion by Dan Simmons.md", Title: "Hyperion",
		Authors: []string{"Dan Simmons"}, Status: "reading", Rating: &rating,
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
