package body

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParse_Empty(t *testing.T) {
	b, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 0 {
		t.Errorf("expected no blocks for empty input, got %d", len(b.Blocks))
	}
	out := b.Serialize()
	if len(out) != 0 {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestParse_TitleOnly(t *testing.T) {
	input := []byte("# Hyperion\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(b.Blocks))
	}
	if b.Blocks[0].Kind != KindH1 {
		t.Errorf("expected KindH1, got %v", b.Blocks[0].Kind)
	}
	h := b.Blocks[0].Parsed.(*H1Parsed)
	if h.Title != "Hyperion" {
		t.Errorf("Title got %q", h.Title)
	}
	if h.Rating != nil {
		t.Errorf("Rating should be nil, got %v", *h.Rating)
	}
	if !bytes.Equal(b.Serialize(), input) {
		t.Errorf("round-trip failed:\nwant %q\ngot  %q", input, b.Serialize())
	}
}

func TestParse_AllRecognizedSections(t *testing.T) {
	input := []byte(`# Hyperion

Rating — 4/5

## Key Ideas / Takeaways

- Hegemony is metastable
- Fatline communication

## Notes

Dense first half. Pays off.

## Quotes & Highlights

> "Pain and love"

## Actions

- Read Fall of Hyperion next

## Related

- [[Dune]]

## Reading Timeline

- 2025-03-09 — Started reading (physical)
- 2025-04-02 — Finished, rated 4
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	expected := []Kind{
		KindH1, KindKeyIdeas, KindNotes, KindQuotes,
		KindActions, KindRelated, KindTimeline,
	}
	if len(b.Blocks) != len(expected) {
		t.Fatalf("block count got %d want %d: %+v", len(b.Blocks), len(expected), kindsOf(b))
	}
	for i, want := range expected {
		if b.Blocks[i].Kind != want {
			t.Errorf("block %d kind got %v want %v", i, b.Blocks[i].Kind, want)
		}
	}
	if !bytes.Equal(b.Serialize(), input) {
		t.Errorf("round-trip failed")
	}

	h1 := b.Blocks[0].Parsed.(*H1Parsed)
	if h1.Title != "Hyperion" {
		t.Errorf("H1 title got %q", h1.Title)
	}
	if h1.Rating == nil || *h1.Rating != 4 {
		t.Errorf("H1 rating got %v", h1.Rating)
	}

	ki := b.Blocks[1].Parsed.(*ListParsed)
	if len(ki.Items) != 2 {
		t.Errorf("key-ideas item count got %d", len(ki.Items))
	}

	tl := b.Blocks[6].Parsed.(*TimelineParsed)
	if len(tl.Events) != 2 {
		t.Fatalf("timeline event count got %d", len(tl.Events))
	}
	if tl.Events[0].Date.Format("2006-01-02") != "2025-03-09" {
		t.Errorf("first event date got %s", tl.Events[0].Date.Format("2006-01-02"))
	}
}

func TestParse_UnrecognizedSectionPreserved(t *testing.T) {
	input := []byte(`# A

## My Custom Thoughts

Some private stuff.

## Notes

Recognized.
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 3 {
		t.Fatalf("block count got %d", len(b.Blocks))
	}
	if b.Blocks[1].Kind != KindUnknown {
		t.Errorf("expected KindUnknown for custom section, got %v", b.Blocks[1].Kind)
	}
	if b.Blocks[2].Kind != KindNotes {
		t.Errorf("expected KindNotes, got %v", b.Blocks[2].Kind)
	}
	if !bytes.Equal(b.Serialize(), input) {
		t.Errorf("unrecognized content lost on round-trip")
	}
}

func TestParse_InterleavedSections(t *testing.T) {
	input := []byte(`## Notes

n

## Something Else

x

## Reading Timeline

- 2025-01-01 — Started
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Blocks) != 3 {
		t.Fatalf("block count got %d: %v", len(b.Blocks), kindsOf(b))
	}
	kinds := []Kind{KindNotes, KindUnknown, KindTimeline}
	for i, k := range kinds {
		if b.Blocks[i].Kind != k {
			t.Errorf("block %d kind got %v want %v", i, b.Blocks[i].Kind, k)
		}
	}
	if !bytes.Equal(b.Serialize(), input) {
		t.Error("round-trip failed with interleaved unrecognized section")
	}
}

func TestParse_TimelineFreeform(t *testing.T) {
	input := []byte(`## Reading Timeline

- 2025-03-09 — Started
- random user note line
- 2025-04-02 — Finished
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	tl := b.Blocks[0].Parsed.(*TimelineParsed)
	if len(tl.Events) != 3 {
		t.Fatalf("event count got %d", len(tl.Events))
	}
	if !tl.Events[1].Date.IsZero() {
		t.Errorf("freeform line should have zero Date, got %v", tl.Events[1].Date)
	}
	if !bytes.Contains(tl.Events[1].Raw, []byte("random user note line")) {
		t.Errorf("freeform raw lost: %q", tl.Events[1].Raw)
	}
}

func TestParse_CRLF(t *testing.T) {
	input := []byte("# A\r\n\r\n## Notes\r\n\r\nBody text.\r\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Serialize(), input) {
		t.Errorf("CRLF round-trip failed:\nwant %q\ngot  %q", input, b.Serialize())
	}
}

func TestParse_RatingLine_Invalid(t *testing.T) {
	input := []byte("# A\n\nRating — 0/5\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	h := b.Blocks[0].Parsed.(*H1Parsed)
	if h.Rating != nil {
		t.Errorf("Rating 0/5 should not parse, got %v", *h.Rating)
	}
}

func TestParse_RatingLine_EnDash(t *testing.T) {
	// Tolerate en-dash (U+2013) even though spec uses em-dash (U+2014).
	input := []byte("# A\n\nRating \u2013 3/5\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	h := b.Blocks[0].Parsed.(*H1Parsed)
	if h.Rating == nil || *h.Rating != 3 {
		t.Errorf("Rating got %v, want *3", h.Rating)
	}
}

func TestSerialize_RoundtripByteEquivalent_UntouchedInput(t *testing.T) {
	fixtures := [][]byte{
		[]byte("# Just a title\n"),
		[]byte(`# A

Rating — 5/5

## Notes

Hello.

## Reading Timeline

- 2025-01-01 — Started
- 2025-02-02 — Finished
`),
		// Quirks users care about: trailing whitespace, multiple blank
		// lines, unrecognized sections with CRLF.
		[]byte("# B\n\n## Custom\n\nfoo  \n\n\n## Notes\n\nbar\n"),
	}
	for i, in := range fixtures {
		b, err := Parse(in)
		if err != nil {
			t.Fatalf("fixture %d parse: %v", i, err)
		}
		if out := b.Serialize(); !bytes.Equal(out, in) {
			t.Errorf("fixture %d round-trip differs:\nwant %q\ngot  %q", i, in, out)
		}
	}
}

func TestSerialize_AppendTimelineEvent_OnlyTimelineBlockChanges(t *testing.T) {
	input := []byte(`# A

Rating — 4/5

## Notes

Static prose.

## Reading Timeline

- 2025-03-09 — Started
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	when, _ := time.Parse("2006-01-02", "2025-04-02")
	b.AppendTimelineEvent(when, "Finished")
	out := b.Serialize()
	if bytes.Equal(out, input) {
		t.Fatal("expected serialization to differ after AppendTimelineEvent")
	}
	// Bytes up through the Notes block should be byte-identical.
	notesEnd := bytes.Index(input, []byte("## Reading Timeline"))
	if notesEnd < 0 {
		t.Fatal("fixture should contain timeline heading")
	}
	if !bytes.Equal(out[:notesEnd], input[:notesEnd]) {
		t.Errorf("bytes before timeline block changed:\nwant %q\ngot  %q", input[:notesEnd], out[:notesEnd])
	}
	if !bytes.Contains(out, []byte("2025-04-02 — Finished")) {
		t.Errorf("new event missing: %q", out)
	}
	if !bytes.Contains(out, []byte("2025-03-09 — Started")) {
		t.Errorf("original event lost: %q", out)
	}
}

func TestEnsureSection_InsertsAtCanonicalPosition(t *testing.T) {
	// Body has only H1, Notes, Timeline. Inserting Actions should place
	// it between Notes (canonical=2) and Timeline (canonical=6).
	input := []byte(`# A

## Notes

n

## Reading Timeline

- 2025-01-01 — Started
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	b.EnsureSection(KindActions)
	out := b.Serialize()

	notesIdx := strings.Index(string(out), "## Notes")
	actionsIdx := strings.Index(string(out), "## Actions")
	timelineIdx := strings.Index(string(out), "## Reading Timeline")
	if notesIdx < 0 || actionsIdx < 0 || timelineIdx < 0 {
		t.Fatalf("sections missing in output:\n%s", out)
	}
	if !(notesIdx < actionsIdx && actionsIdx < timelineIdx) {
		t.Errorf("canonical order violated: notes=%d actions=%d timeline=%d\n%s",
			notesIdx, actionsIdx, timelineIdx, out)
	}
}

func TestEnsureSection_Idempotent(t *testing.T) {
	input := []byte("# A\n\n## Notes\n\nn\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	b.EnsureSection(KindNotes)
	if len(b.Blocks) != 2 {
		t.Errorf("EnsureSection on existing kind should be no-op, got %d blocks", len(b.Blocks))
	}
	if !bytes.Equal(b.Serialize(), input) {
		t.Errorf("idempotent ensure changed serialization")
	}
}

// Session 15: body.SetRating (H1-line variant) removed; the Rating is
// now a dedicated `## Rating` section written via SetRatingFromFrontmatter.
// Detailed tests for the new path live in rating_test.go; body_test.go
// retains the parse-legacy tests so existing notes round-trip cleanly.

func kindsOf(b *Body) []Kind {
	out := make([]Kind, len(b.Blocks))
	for i := range b.Blocks {
		out[i] = b.Blocks[i].Kind
	}
	return out
}
