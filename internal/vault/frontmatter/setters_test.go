package frontmatter

import (
	"strings"
	"testing"
)

func TestSetTag_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetTag("📚Book")
	out, err := f.Serialize(nil)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	f2, _, err := Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if f2.Tag() != "📚Book" {
		t.Errorf("Tag got %q", f2.Tag())
	}
}

func TestSetSubtitle_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetSubtitle("A Memoir")
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.Subtitle() != "A Memoir" {
		t.Errorf("Subtitle got %q", f2.Subtitle())
	}
}

func TestSetAuthors_Multi(t *testing.T) {
	f := NewEmpty()
	f.SetAuthors([]string{"Dan Simmons", "Co Author"})
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	got := f2.Authors()
	if len(got) != 2 || got[0] != "Dan Simmons" || got[1] != "Co Author" {
		t.Errorf("Authors got %v", got)
	}
}

func TestSetAuthors_Empty(t *testing.T) {
	f := NewEmpty()
	f.SetAuthors(nil)
	out, _ := f.Serialize(nil)
	// An empty sequence should serialize as "[]" flow-style (matches the
	// Obsidian Book Search template's empty-array convention).
	if !strings.Contains(string(out), "authors: []") {
		t.Errorf("expected empty authors to serialize as []; got:\n%s", out)
	}
	f2, _, _ := Parse(out)
	if got := f2.Authors(); len(got) != 0 {
		t.Errorf("expected empty Authors, got %v", got)
	}
}

func TestSetCategories_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetCategories([]string{"sci-fi", "favorites"})
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if got := f2.Categories(); len(got) != 2 || got[0] != "sci-fi" {
		t.Errorf("Categories got %v", got)
	}
}

func TestSetPublisher_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetPublisher("Bantam Spectra")
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.Publisher() != "Bantam Spectra" {
		t.Errorf("Publisher got %q", f2.Publisher())
	}
}

func TestSetPublish_FreeformDate(t *testing.T) {
	f := NewEmpty()
	f.SetPublish("1989")
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.Publish() != "1989" {
		t.Errorf("Publish got %q", f2.Publish())
	}
}

func TestSetTotalPages_AndClear(t *testing.T) {
	f := NewEmpty()
	pages := 482
	if err := f.SetTotalPages(&pages); err != nil {
		t.Fatalf("SetTotalPages: %v", err)
	}
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if got := f2.TotalPages(); got == nil || *got != 482 {
		t.Errorf("TotalPages got %v", got)
	}

	// Clear it.
	if err := f2.SetTotalPages(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	out2, _ := f2.Serialize(nil)
	f3, _, _ := Parse(out2)
	if got := f3.TotalPages(); got != nil {
		t.Errorf("expected nil after clear, got %v", got)
	}
}

func TestSetTotalPages_NegativeRejected(t *testing.T) {
	f := NewEmpty()
	neg := -1
	if err := f.SetTotalPages(&neg); err == nil {
		t.Fatal("expected error for negative total_pages")
	}
}

func TestSetISBN_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetISBN("9780553283686")
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.ISBN() != "9780553283686" {
		t.Errorf("ISBN got %q", f2.ISBN())
	}
}

func TestSetCover_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetCover("covers/hyperion.jpg")
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.Cover() != "covers/hyperion.jpg" {
		t.Errorf("Cover got %q", f2.Cover())
	}
}

func TestSetFormat_Valid(t *testing.T) {
	cases := []string{"audiobook", "ebook", "physical"}
	for _, c := range cases {
		f := NewEmpty()
		if err := f.SetFormat(c); err != nil {
			t.Fatalf("SetFormat(%q): %v", c, err)
		}
		out, _ := f.Serialize(nil)
		f2, _, _ := Parse(out)
		if f2.Format() != c {
			t.Errorf("Format(%q) round-trip got %q", c, f2.Format())
		}
	}
}

func TestSetFormat_InvalidRejected(t *testing.T) {
	f := NewEmpty()
	if err := f.SetFormat("vinyl"); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestSetFormat_EmptyClears(t *testing.T) {
	f := NewEmpty()
	_ = f.SetFormat("ebook")
	if err := f.SetFormat(""); err != nil {
		t.Fatalf("SetFormat(\"\"): %v", err)
	}
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.Format() != "" {
		t.Errorf("expected empty format after clear, got %q", f2.Format())
	}
}

func TestSetSource_RoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetSource("Audible")
	out, _ := f.Serialize(nil)
	f2, _, _ := Parse(out)
	if f2.Source() != "Audible" {
		t.Errorf("Source got %q", f2.Source())
	}
}

func TestSetters_MutatedKeys(t *testing.T) {
	// Every setter should register the key in MutatedKeys so SaveFrontmatter
	// replays just those fields onto a fresh disk read.
	f := NewEmpty()
	f.SetTag("📚Book")
	f.SetSubtitle("sub")
	f.SetAuthors([]string{"A"})
	f.SetCategories([]string{"c"})
	f.SetPublisher("p")
	f.SetPublish("2024")
	pages := 100
	_ = f.SetTotalPages(&pages)
	f.SetISBN("isbn")
	f.SetCover("cov")
	_ = f.SetFormat("ebook")
	f.SetSource("src")

	want := []string{
		KeyTag, KeySubtitle, KeyAuthors, KeyCategories, KeyPublisher,
		KeyPublish, KeyTotalPages, KeyISBN, KeyCover, KeyFormat, KeySource,
	}
	got := f.MutatedKeys()
	set := make(map[string]bool, len(got))
	for _, k := range got {
		set[k] = true
	}
	for _, k := range want {
		if !set[k] {
			t.Errorf("MutatedKeys missing %q (have %v)", k, got)
		}
	}
}
