package frontmatter

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParse_NoFrontmatter(t *testing.T) {
	input := []byte("# Just a heading\n\nNo frontmatter here.\n")
	_, body, err := Parse(input)
	if !errors.Is(err, ErrNoFrontmatter) {
		t.Fatalf("expected ErrNoFrontmatter, got %v", err)
	}
	if !bytes.Equal(body, input) {
		t.Errorf("body should equal original input when no frontmatter")
	}
}

func TestParse_EmptyFrontmatter(t *testing.T) {
	input := []byte("---\n---\n# Body\n")
	f, body, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Title() != "" {
		t.Errorf("empty frontmatter should have no title, got %q", f.Title())
	}
	if string(body) != "# Body\n" {
		t.Errorf("body got %q", body)
	}
}

func TestParse_Minimal(t *testing.T) {
	input := []byte(`---
title: Hyperion
authors:
  - Dan Simmons
---
# Hyperion
`)
	f, body, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Title() != "Hyperion" {
		t.Errorf("Title got %q", f.Title())
	}
	authors := f.Authors()
	if len(authors) != 1 || authors[0] != "Dan Simmons" {
		t.Errorf("Authors got %v", authors)
	}
	if string(body) != "# Hyperion\n" {
		t.Errorf("body got %q", body)
	}
}

func TestParse_FullSchema(t *testing.T) {
	input := []byte(`---
tag: 📚Book
title: Hyperion
subtitle: ""
authors:
  - Dan Simmons
categories:
  - science-fiction
  - space-opera
publisher: Doubleday
publish: "1989-05-26"
total_pages: 482
isbn: "9780385249492"
cover: ""
format: physical
source: Library
started:
  - 2025-03-09
finished:
  - 2025-04-02
rating: 4
status: finished
read_count: 1
---
# Hyperion
Body text.
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Tag() != "📚Book" {
		t.Errorf("Tag got %q", f.Tag())
	}
	if f.Publisher() != "Doubleday" {
		t.Errorf("Publisher got %q", f.Publisher())
	}
	if p := f.TotalPages(); p == nil || *p != 482 {
		t.Errorf("TotalPages got %v", p)
	}
	if f.Format() != "physical" {
		t.Errorf("Format got %q", f.Format())
	}
	cats := f.Categories()
	if len(cats) != 2 || cats[0] != "science-fiction" || cats[1] != "space-opera" {
		t.Errorf("Categories got %v", cats)
	}
	if r := f.Rating(); r == nil || !r.HasOverride() || *r.Overall != 4 {
		t.Errorf("Rating got %v", r)
	}
	if f.Status() != "finished" {
		t.Errorf("Status got %q", f.Status())
	}
	if f.ReadCount() != 1 {
		t.Errorf("ReadCount got %d", f.ReadCount())
	}
	started := f.Started()
	if len(started) != 1 || !started[0].Equal(mustParseDate(t, "2025-03-09")) {
		t.Errorf("Started got %v", started)
	}
	finished := f.Finished()
	if len(finished) != 1 || !finished[0].Equal(mustParseDate(t, "2025-04-02")) {
		t.Errorf("Finished got %v", finished)
	}
}

func TestParse_MalformedYAML(t *testing.T) {
	input := []byte("---\n[not: valid yaml\n---\nbody\n")
	_, _, err := Parse(input)
	if err == nil {
		t.Fatal("expected YAML parse error")
	}
}

func TestParse_NoClosingDelimiter(t *testing.T) {
	input := []byte("---\ntitle: Hyperion\n# never closed\n")
	_, _, err := Parse(input)
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
	if !strings.Contains(err.Error(), "closing") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_ClosingDelimiterAtEOF(t *testing.T) {
	// No trailing newline after the closing ---
	input := []byte("---\ntitle: Hyperion\n---")
	f, body, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Title() != "Hyperion" {
		t.Errorf("Title got %q", f.Title())
	}
	if len(body) != 0 {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestParse_CRLF(t *testing.T) {
	input := []byte("---\r\ntitle: Hyperion\r\n---\r\n# Body\r\n")
	f, body, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Title() != "Hyperion" {
		t.Errorf("Title got %q", f.Title())
	}
	if string(body) != "# Body\r\n" {
		t.Errorf("body got %q", body)
	}
	if !f.useCRLF {
		t.Error("useCRLF flag should be set")
	}
}

func TestParse_DelimiterInBody(t *testing.T) {
	// A "---" in the body (Markdown horizontal rule) must not be mistaken
	// for the closing delimiter.
	input := []byte(`---
title: Hyperion
---
# Body

Some text

---

More text after rule.
`)
	f, body, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Title() != "Hyperion" {
		t.Errorf("Title got %q", f.Title())
	}
	if !bytes.Contains(body, []byte("More text after rule.")) {
		t.Errorf("body truncated: %q", body)
	}
}

func TestParse_BodyPreservedVerbatim(t *testing.T) {
	// The body must come out byte-identical — especially tricky bits like
	// trailing whitespace, multiple blank lines, and CRLF.
	body := "# Hyperion\n\n   Indented text\n\n---\nHorizontal rule\n\n\n"
	input := []byte("---\ntitle: Hyperion\n---\n" + body)
	_, gotBody, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if string(gotBody) != body {
		t.Errorf("body mutated.\ngot:  %q\nwant: %q", gotBody, body)
	}
}

func TestSerialize_RoundTripAfterSetRating(t *testing.T) {
	// Rating change keeps non-rating lines byte-identical; the rating
	// region itself is rewritten as a YAML map (post-S15).
	input := []byte(`---
title: Hyperion
authors:
  - Dan Simmons
rating: 4
status: finished
---
body
`)
	f, body, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	over := 5.0
	if err := f.SetRating(&Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	out, err := f.Serialize(body)
	if err != nil {
		t.Fatal(err)
	}
	// Post-condition: every non-rating line from input survives in out.
	for _, line := range []string{
		"title: Hyperion", "authors:", "- Dan Simmons", "status: finished",
	} {
		if !strings.Contains(string(out), line) {
			t.Errorf("line %q missing after rating rewrite:\n%s", line, out)
		}
	}
	// And the new map shape is present.
	if !strings.Contains(string(out), "rating:") {
		t.Errorf("rating key missing")
	}
	if !strings.Contains(string(out), "overall: 5") {
		t.Errorf("overall: 5 missing:\n%s", out)
	}
}

func TestSetStatus_Enum(t *testing.T) {
	f := NewEmpty()
	for _, s := range []string{"unread", "reading", "paused", "finished", "dnf"} {
		if err := f.SetStatus(s); err != nil {
			t.Errorf("SetStatus(%q): %v", s, err)
		}
	}
	for _, s := range []string{"read", "completed", "", "UNREAD", "dropped"} {
		if err := f.SetStatus(s); err == nil {
			t.Errorf("SetStatus(%q) should fail", s)
		}
	}
}

func TestAppendStarted_CreatesField(t *testing.T) {
	f := NewEmpty()
	f.AppendStarted(mustParseDate(t, "2025-03-09"))
	f.AppendStarted(mustParseDate(t, "2025-11-15"))

	dates := f.Started()
	if len(dates) != 2 {
		t.Fatalf("expected 2 dates, got %d: %v", len(dates), dates)
	}
	if !dates[0].Equal(mustParseDate(t, "2025-03-09")) {
		t.Errorf("first date got %v", dates[0])
	}
	if !dates[1].Equal(mustParseDate(t, "2025-11-15")) {
		t.Errorf("second date got %v", dates[1])
	}
}

func TestAppendFinished_ExtendsExistingArray(t *testing.T) {
	input := []byte(`---
title: Hyperion
started:
  - 2025-03-09
finished:
  - 2025-04-02
---
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	f.AppendFinished(mustParseDate(t, "2025-12-30"))
	got := f.Finished()
	if len(got) != 2 {
		t.Fatalf("expected 2 dates, got %d", len(got))
	}
	if !got[1].Equal(mustParseDate(t, "2025-12-30")) {
		t.Errorf("appended date got %v", got[1])
	}
}

func TestSetMissingField_AppendsAtEnd(t *testing.T) {
	input := []byte(`---
title: Hyperion
status: finished
---
body
`)
	f, body, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	over := 5.0
	if err := f.SetRating(&Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	out, err := f.Serialize(body)
	if err != nil {
		t.Fatal(err)
	}
	// Original field order preserved, rating appended at end of
	// frontmatter (before the closing ---).
	lines := strings.Split(string(out), "\n")
	var titleIdx, statusIdx, ratingIdx int = -1, -1, -1
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "title:"):
			titleIdx = i
		case strings.HasPrefix(l, "status:"):
			statusIdx = i
		case strings.HasPrefix(l, "rating:"):
			ratingIdx = i
		}
	}
	if titleIdx < 0 || statusIdx < 0 || ratingIdx < 0 {
		t.Fatalf("missing expected fields in output:\n%s", out)
	}
	if !(titleIdx < statusIdx && statusIdx < ratingIdx) {
		t.Errorf("unexpected field order: title=%d status=%d rating=%d\n%s",
			titleIdx, statusIdx, ratingIdx, out)
	}
}

func TestSerialize_PreservesCRLF(t *testing.T) {
	input := []byte("---\r\ntitle: Hyperion\r\n---\r\n# Body\r\n")
	f, body, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.Serialize(body)
	if err != nil {
		t.Fatal(err)
	}
	// Frontmatter block must use CRLF line endings end-to-end.
	firstBodyIdx := bytes.Index(out, []byte("# Body"))
	if firstBodyIdx < 0 {
		t.Fatalf("body missing: %q", out)
	}
	fmBlock := out[:firstBodyIdx]
	if bytes.Contains(fmBlock, []byte("\n")) && !bytes.Contains(fmBlock, []byte("\r\n")) {
		t.Errorf("frontmatter block lost CRLF: %q", fmBlock)
	}
}

func TestGetters_MissingFieldsReturnZero(t *testing.T) {
	f := NewEmpty()
	if f.Title() != "" {
		t.Errorf("empty title expected")
	}
	if f.TotalPages() != nil {
		t.Errorf("TotalPages should be nil")
	}
	if f.Rating() != nil {
		t.Errorf("Rating should be nil")
	}
	if f.ReadCount() != 0 {
		t.Errorf("ReadCount should default to 0")
	}
	if len(f.Authors()) != 0 {
		t.Errorf("Authors should be empty")
	}
	if len(f.Started()) != 0 {
		t.Errorf("Started should be empty")
	}
}

func TestGetters_MalformedDateSkipped(t *testing.T) {
	input := []byte(`---
started:
  - 2025-03-09
  - not-a-date
  - 2025-11-15
---
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	got := f.Started()
	if len(got) != 2 {
		t.Errorf("expected 2 valid dates (malformed skipped), got %d: %v", len(got), got)
	}
}

func TestSeries_GetFromYAML(t *testing.T) {
	input := []byte(`---
title: The Way of Kings
series: The Stormlight Archive
series_index: 1
---
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Series(); got != "The Stormlight Archive" {
		t.Errorf("Series got %q", got)
	}
	idx := f.SeriesIndex()
	if idx == nil || *idx != 1.0 {
		t.Errorf("SeriesIndex got %v, want *1.0", idx)
	}
}

func TestSeries_FractionalIndex(t *testing.T) {
	input := []byte(`---
title: Edgedancer
series: The Stormlight Archive
series_index: 2.5
---
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	idx := f.SeriesIndex()
	if idx == nil || *idx != 2.5 {
		t.Errorf("SeriesIndex got %v, want *2.5", idx)
	}
}

func TestSeries_Absent(t *testing.T) {
	f := NewEmpty()
	if got := f.Series(); got != "" {
		t.Errorf("Series got %q, want empty", got)
	}
	if idx := f.SeriesIndex(); idx != nil {
		t.Errorf("SeriesIndex got %v, want nil", idx)
	}
}

func TestSeries_SetAndRoundTrip(t *testing.T) {
	input := []byte(`---
title: Mistborn
---
body
`)
	f, body, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	f.SetSeries("Mistborn Era One")
	idx := 1.0
	if err := f.SetSeriesIndex(&idx); err != nil {
		t.Fatal(err)
	}
	out, err := f.Serialize(body)
	if err != nil {
		t.Fatal(err)
	}

	// Parse the output and assert round-trip.
	f2, _, err := Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got := f2.Series(); got != "Mistborn Era One" {
		t.Errorf("Series round-trip got %q", got)
	}
	got := f2.SeriesIndex()
	if got == nil || *got != 1.0 {
		t.Errorf("SeriesIndex round-trip got %v", got)
	}
	// Whole-number index should emit without a decimal.
	if !bytes.Contains(out, []byte("series_index: 1\n")) && !bytes.Contains(out, []byte("series_index: 1\r\n")) {
		t.Errorf("expected bare integer emission for series_index: 1, got:\n%s", out)
	}
}

func TestSeries_SetFractionalRoundTrip(t *testing.T) {
	f := NewEmpty()
	f.SetSeries("Stormlight")
	idx := 2.5
	if err := f.SetSeriesIndex(&idx); err != nil {
		t.Fatal(err)
	}
	out, err := f.Serialize(nil)
	if err != nil {
		t.Fatal(err)
	}
	f2, _, err := Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	got := f2.SeriesIndex()
	if got == nil || *got != 2.5 {
		t.Errorf("SeriesIndex got %v, want *2.5\nemitted:\n%s", got, out)
	}
}

func TestSetSeriesIndex_Clear(t *testing.T) {
	input := []byte(`---
series: Stormlight
series_index: 1
---
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetSeriesIndex(nil); err != nil {
		t.Fatal(err)
	}
	if idx := f.SeriesIndex(); idx != nil {
		t.Errorf("after clear, SeriesIndex got %v, want nil", idx)
	}
}

func TestMutatedKeys_TracksSetters(t *testing.T) {
	input := []byte(`---
title: Hyperion
rating: 3
---
`)
	f, _, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	// No mutations yet.
	if keys := f.MutatedKeys(); len(keys) != 0 {
		t.Errorf("fresh parse should report 0 mutations, got %v", keys)
	}
	over := 5.0
	if err := f.SetRating(&Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	f.AppendStarted(mustParseDate(t, "2025-04-02"))

	seen := map[string]bool{}
	for _, k := range f.MutatedKeys() {
		seen[k] = true
	}
	if !seen[KeyRating] {
		t.Errorf("rating not tracked")
	}
	if !seen[KeyStarted] {
		t.Errorf("started not tracked")
	}
	if seen[KeyTitle] {
		t.Errorf("title should not be tracked (never set)")
	}
}

func TestSetSeriesIndex_Rejects(t *testing.T) {
	f := NewEmpty()
	neg := -1.0
	if err := f.SetSeriesIndex(&neg); err == nil {
		t.Error("negative index should be rejected")
	}
	nan := math.NaN()
	if err := f.SetSeriesIndex(&nan); err == nil {
		t.Error("NaN should be rejected")
	}
	inf := math.Inf(1)
	if err := f.SetSeriesIndex(&inf); err == nil {
		t.Error("+Inf should be rejected")
	}
}

// mustParseDate parses "YYYY-MM-DD" or fails the test.
func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(DateFormat, s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

