package paths

import (
	"errors"
	"testing"
	"testing/quick"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name       string
		filename   string
		wantTitle  string
		wantAuthor string
	}{
		{"plain", "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons"},
		{"title with ' by '", "Learning by Doing by Jane Smith.md", "Learning by Doing", "Jane Smith"},
		{"sanitized colon", "Dune\uA789 Messiah by Frank Herbert.md", "Dune\uA789 Messiah", "Frank Herbert"},
		{"author with middle initial", "Foundation by Isaac Asimov.md", "Foundation", "Isaac Asimov"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			title, author, err := Parse(c.filename)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if title != c.wantTitle {
				t.Errorf("title: got %q want %q", title, c.wantTitle)
			}
			if author != c.wantAuthor {
				t.Errorf("author: got %q want %q", author, c.wantAuthor)
			}
		})
	}
}

func TestParse_NonCanonical(t *testing.T) {
	cases := []struct {
		name     string
		filename string
	}{
		{"no .md extension", "Hyperion by Dan Simmons"},
		{"no separator", "Hyperion - Dan Simmons.md"},
		{"separator variant without spaces", "Hyperionby Dan Simmons.md"},
		{"empty title", " by Dan Simmons.md"},
		{"empty author", "Hyperion by .md"},
		{"only extension", ".md"},
		{"completely empty", ""},
		{"only separator", " by .md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := Parse(c.filename)
			if !errors.Is(err, ErrNonCanonical) {
				t.Errorf("Parse(%q): expected ErrNonCanonical, got %v", c.filename, err)
			}
		})
	}
}

// TestParseGenerateRoundTrip is a property test: for any (title, author)
// that Generate accepts, Parse must recover the same pair. The round-trip
// equality is over the *sanitized* forms, which is the point of having a
// normalized on-disk representation.
func TestParseGenerateRoundTrip(t *testing.T) {
	f := func(title, author string) bool {
		// Skip inputs that Generate would reject (empty after sanitize);
		// those are validated independently in TestGenerate_EmptyTitle etc.
		if SanitizeFilename(title) == "" || SanitizeFilename(author) == "" {
			return true
		}
		filename, err := Generate(title, author)
		if err != nil {
			return false
		}
		gotTitle, gotAuthor, err := Parse(filename)
		if err != nil {
			t.Logf("Generate(%q, %q) = %q; Parse returned %v", title, author, filename, err)
			return false
		}
		wantTitle := SanitizeFilename(title)
		wantAuthor := SanitizeFilename(author)
		if gotTitle != wantTitle {
			t.Logf("title round-trip: generate input %q, want sanitized %q, got %q", title, wantTitle, gotTitle)
			return false
		}
		if gotAuthor != wantAuthor {
			t.Logf("author round-trip: generate input %q, want sanitized %q, got %q", author, wantAuthor, gotAuthor)
			return false
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Error(err)
	}
}
