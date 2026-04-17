package body

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetNotes_CreatesSection(t *testing.T) {
	b := &Body{}
	b.SetNotes("A short review.")
	out := b.Serialize()
	if !strings.Contains(string(out), "## Notes\n") {
		t.Errorf("expected Notes heading; got:\n%s", out)
	}
	if !strings.Contains(string(out), "A short review.") {
		t.Errorf("expected review text; got:\n%s", out)
	}
}

func TestSetNotes_ReplacesExisting(t *testing.T) {
	b, err := Parse([]byte("# Hyperion\n\n## Notes\n\noriginal review\n"))
	if err != nil {
		t.Fatal(err)
	}
	b.SetNotes("overwritten review")
	out := b.Serialize()
	if strings.Contains(string(out), "original review") {
		t.Errorf("original text should be gone; got:\n%s", out)
	}
	if !strings.Contains(string(out), "overwritten review") {
		t.Errorf("new text missing; got:\n%s", out)
	}
}

func TestAppendNotes_PreservesExisting(t *testing.T) {
	b, err := Parse([]byte("# Hyperion\n\n## Notes\n\nuser notes here\n"))
	if err != nil {
		t.Fatal(err)
	}
	b.AppendNotes("_Imported from Goodreads on 2026-04-17_\n\n> review body")
	out := string(b.Serialize())
	if !strings.Contains(out, "user notes here") {
		t.Errorf("existing content lost; got:\n%s", out)
	}
	if !strings.Contains(out, "_Imported from Goodreads on 2026-04-17_") {
		t.Errorf("provenance line missing; got:\n%s", out)
	}
	// Blank-line separator between old and new.
	if !strings.Contains(out, "user notes here\n\n_Imported from Goodreads") {
		t.Errorf("expected blank-line separator; got:\n%s", out)
	}
}

func TestAppendNotes_EmptySection(t *testing.T) {
	b := &Body{}
	b.EnsureSection(KindNotes)
	b.AppendNotes("first content")
	out := string(b.Serialize())
	if !strings.Contains(out, "first content") {
		t.Errorf("expected appended content; got:\n%s", out)
	}
	// Should NOT start with a blank line prefix when section is empty.
	if strings.Contains(out, "\n\n\nfirst content") {
		t.Errorf("unexpected extra blank line; got:\n%s", out)
	}
}

func TestNotes_Getter(t *testing.T) {
	t.Run("absent section", func(t *testing.T) {
		b := &Body{}
		if got := b.Notes(); got != "" {
			t.Errorf("empty body Notes() = %q, want \"\"", got)
		}
	})
	t.Run("populated section", func(t *testing.T) {
		b, err := Parse([]byte("# Hyperion\n\n## Notes\n\nhello world\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := b.Notes(); !strings.Contains(got, "hello world") {
			t.Errorf("Notes() = %q, missing expected text", got)
		}
	})
	t.Run("round-trips SetNotes", func(t *testing.T) {
		b := &Body{}
		b.SetNotes("fresh text")
		if got := b.Notes(); got != "fresh text" {
			t.Errorf("Notes() = %q, want %q", got, "fresh text")
		}
	})
}

func TestSetNotes_DirtyRegeneratesOnlyNotesBlock(t *testing.T) {
	input := []byte("# Hyperion\n\nRating — 4/5\n\n## Notes\n\nold\n\n## Actions\n\n- TODO\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	b.SetNotes("new")

	out := b.Serialize()
	// The Actions block wasn't touched — its bytes should be byte-identical
	// to the source for the section they occupy.
	if !bytes.Contains(out, []byte("## Actions\n\n- TODO\n")) {
		t.Errorf("Actions block should be untouched; got:\n%s", out)
	}
	if bytes.Contains(out, []byte("old")) {
		t.Errorf("old notes content survived; got:\n%s", out)
	}
}
