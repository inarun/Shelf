package frontmatter

import (
	"bytes"
	"errors"
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
	if r := f.Rating(); r == nil || *r != 4 {
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
	// Rating change updates only the rating line; every other line
	// byte-equivalent.
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
	r := 5
	if err := f.SetRating(&r); err != nil {
		t.Fatal(err)
	}
	out, err := f.Serialize(body)
	if err != nil {
		t.Fatal(err)
	}
	// Post-condition: the only line that differs is the rating one.
	diff := diffLines(string(input), string(out))
	if len(diff) != 1 {
		t.Errorf("expected exactly 1 changed line, got %d:\n%v", len(diff), diff)
	}
	if len(diff) > 0 && !strings.Contains(diff[0], "rating") {
		t.Errorf("changed line isn't the rating: %q", diff[0])
	}
}

func TestSetRating_OutOfRange(t *testing.T) {
	f := NewEmpty()
	bad := 6
	if err := f.SetRating(&bad); err == nil {
		t.Error("expected error for rating 6")
	}
	low := 0
	if err := f.SetRating(&low); err == nil {
		t.Error("expected error for rating 0")
	}
	// nil clears; valid
	if err := f.SetRating(nil); err != nil {
		t.Errorf("nil rating should clear, got: %v", err)
	}
	// 1..5 valid
	for _, v := range []int{1, 2, 3, 4, 5} {
		val := v
		if err := f.SetRating(&val); err != nil {
			t.Errorf("rating %d should be valid, got: %v", v, err)
		}
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
	r := 5
	if err := f.SetRating(&r); err != nil {
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

// mustParseDate parses "YYYY-MM-DD" or fails the test.
func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(DateFormat, s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

// diffLines returns the lines present in b that are not in a, at the
// same index. Simple enough for small fixtures; not a general LCS.
func diffLines(a, b string) []string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	max := len(al)
	if len(bl) > max {
		max = len(bl)
	}
	var out []string
	for i := 0; i < max; i++ {
		var av, bv string
		if i < len(al) {
			av = al[i]
		}
		if i < len(bl) {
			bv = bl[i]
		}
		if av != bv {
			out = append(out, "- "+av+"\n+ "+bv)
		}
	}
	return out
}
