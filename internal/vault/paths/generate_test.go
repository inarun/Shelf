package paths

import (
	"errors"
	"testing"
)

func TestGenerate(t *testing.T) {
	cases := []struct {
		name   string
		title  string
		author string
		want   string
	}{
		{"plain", "Hyperion", "Dan Simmons", "Hyperion by Dan Simmons.md"},
		{"colon in title", "Dune: Messiah", "Frank Herbert", "Dune\uA789 Messiah by Frank Herbert.md"},
		{"slash in title", "AC/DC: The Book", "Biographer", "AC\u2044DC\uA789 The Book by Biographer.md"},
		{"extra whitespace collapsed", "  Hyperion   ", "Dan  Simmons", "Hyperion by Dan Simmons.md"},
		{"title contains ' by '", "Learning by Doing", "Jane Smith", "Learning by Doing by Jane Smith.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Generate(c.title, c.author)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if got != c.want {
				t.Errorf("Generate(%q, %q):\n got  %q\n want %q", c.title, c.author, got, c.want)
			}
		})
	}
}

func TestGenerate_EmptyTitle(t *testing.T) {
	_, err := Generate("", "Dan Simmons")
	if !errors.Is(err, ErrEmptyComponent) {
		t.Errorf("expected ErrEmptyComponent, got %v", err)
	}
}

func TestGenerate_EmptyAuthor(t *testing.T) {
	_, err := Generate("Hyperion", "")
	if !errors.Is(err, ErrEmptyComponent) {
		t.Errorf("expected ErrEmptyComponent, got %v", err)
	}
}

func TestGenerate_TitleSanitizesToEmpty(t *testing.T) {
	_, err := Generate("   ", "Dan Simmons")
	if !errors.Is(err, ErrEmptyComponent) {
		t.Errorf("expected ErrEmptyComponent for whitespace-only title, got %v", err)
	}
}

func TestGenerate_TitleWithOnlyNullBytes(t *testing.T) {
	_, err := Generate("\x00\x00", "Dan Simmons")
	if !errors.Is(err, ErrEmptyComponent) {
		t.Errorf("expected ErrEmptyComponent for null-only title, got %v", err)
	}
}
