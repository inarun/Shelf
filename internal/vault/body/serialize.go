package body

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Serialize concatenates each block's serialized form in order. A block
// that hasn't been mutated (dirty==false) emits its Raw bytes verbatim,
// so an unmodified Body round-trips byte-for-byte. Dirty blocks are
// regenerated from their Parsed view using the canonical layout.
func (b *Body) Serialize() []byte {
	var buf bytes.Buffer
	for _, bl := range b.Blocks {
		if !bl.dirty {
			buf.Write(bl.Raw)
			continue
		}
		buf.Write(regenerate(bl))
	}
	return buf.Bytes()
}

// regenerate produces the canonical form of a dirty block. Uses LF line
// endings; a CRLF input file that later gets a dirty block mutated will
// contain mixed endings in the dirty region. This is accepted for v0.1
// because recognized-section regeneration is app output, while user-
// authored (clean) bytes remain untouched.
func regenerate(bl Block) []byte {
	switch bl.Kind {
	case KindH1:
		return regenerateH1(bl.Parsed.(*H1Parsed))
	case KindNotes, KindQuotes:
		return regenerateText(bl.Kind, bl.Parsed.(*TextParsed))
	case KindKeyIdeas, KindActions, KindRelated:
		return regenerateList(bl.Kind, bl.Parsed.(*ListParsed))
	case KindTimeline:
		return regenerateTimeline(bl.Parsed.(*TimelineParsed))
	default:
		return bl.Raw
	}
}

func regenerateH1(p *H1Parsed) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# %s\n", p.Title)
	if p.Rating != nil {
		fmt.Fprintf(&buf, "\nRating — %d/5\n", *p.Rating)
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func regenerateText(kind Kind, p *TextParsed) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "## %s\n\n", canonicalHeading(kind))
	buf.Write(p.Text)
	if !bytes.HasSuffix(p.Text, []byte("\n")) {
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

func regenerateList(kind Kind, p *ListParsed) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "## %s\n\n", canonicalHeading(kind))
	for _, item := range p.Items {
		buf.Write(item)
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func regenerateTimeline(p *TimelineParsed) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "## %s\n\n", canonicalHeading(KindTimeline))
	for _, ev := range p.Events {
		if !ev.Date.IsZero() {
			fmt.Fprintf(&buf, "- %s — %s\n", ev.Date.Format("2006-01-02"), ev.Text)
			continue
		}
		// Freeform line — preserve the original bytes so user-authored
		// timeline entries survive a dirty regeneration unchanged.
		raw := ev.Raw
		if len(raw) == 0 {
			continue
		}
		if !bytes.HasSuffix(raw, []byte("\n")) {
			raw = append(append([]byte(nil), raw...), '\n')
		}
		buf.Write(raw)
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

// AppendTimelineEvent appends a new dated entry to the Reading Timeline
// section, creating the section at its canonical position if absent.
// The block is marked dirty so the next Serialize regenerates it.
func (b *Body) AppendTimelineEvent(when time.Time, text string) {
	b.EnsureSection(KindTimeline)
	idx := b.indexOf(KindTimeline)
	bl := &b.Blocks[idx]
	if bl.Parsed == nil {
		bl.Parsed = &TimelineParsed{}
	}
	p := bl.Parsed.(*TimelineParsed)
	p.Events = append(p.Events, TimelineEvent{
		Date: when,
		Text: strings.TrimSpace(text),
	})
	bl.dirty = true
}

// EnsureSection inserts an empty recognized section at its canonical
// position when absent. No-op when the section is already present or
// the kind is not a recognized H2. An H1 block is not created by this
// helper — use a setter on an existing H1 or a fresh parse to get one.
func (b *Body) EnsureSection(kind Kind) {
	if kind == KindUnknown || kind == KindH1 {
		return
	}
	if b.indexOf(kind) >= 0 {
		return
	}
	newBlock := Block{Kind: kind, dirty: true, Parsed: emptyParsedFor(kind)}
	insertAt := len(b.Blocks)
	order := canonicalOrder(kind)
	for i, existing := range b.Blocks {
		if canonicalOrder(existing.Kind) > order {
			insertAt = i
			break
		}
	}
	b.Blocks = append(b.Blocks, Block{})
	copy(b.Blocks[insertAt+1:], b.Blocks[insertAt:])
	b.Blocks[insertAt] = newBlock
}

func emptyParsedFor(kind Kind) any {
	switch kind {
	case KindNotes, KindQuotes:
		return &TextParsed{}
	case KindKeyIdeas, KindActions, KindRelated:
		return &ListParsed{}
	case KindTimeline:
		return &TimelineParsed{}
	}
	return nil
}

// indexOf returns the first block index for kind, or -1 if absent.
func (b *Body) indexOf(kind Kind) int {
	for i, bl := range b.Blocks {
		if bl.Kind == kind {
			return i
		}
	}
	return -1
}

// SetRating mutates the H1 block's Rating value. If no H1 block exists,
// one is created with an empty title (caller can then SetTitle). Accepts
// nil to clear the rating.
func (b *Body) SetRating(r *int) error {
	if r != nil && (*r < 1 || *r > 5) {
		return fmt.Errorf("body: rating %d out of range 1..5", *r)
	}
	idx := b.indexOf(KindH1)
	if idx < 0 {
		b.Blocks = append([]Block{{
			Kind:   KindH1,
			Parsed: &H1Parsed{Rating: r},
			dirty:  true,
		}}, b.Blocks...)
		return nil
	}
	bl := &b.Blocks[idx]
	if bl.Parsed == nil {
		bl.Parsed = &H1Parsed{}
	}
	p := bl.Parsed.(*H1Parsed)
	if r == nil {
		p.Rating = nil
	} else {
		val := *r
		p.Rating = &val
	}
	bl.dirty = true
	return nil
}

// SetTitle mutates the H1 block's title. Creates an H1 block when none
// exists so the title lands at the top of the body.
func (b *Body) SetTitle(title string) {
	idx := b.indexOf(KindH1)
	if idx < 0 {
		b.Blocks = append([]Block{{
			Kind:   KindH1,
			Parsed: &H1Parsed{Title: title},
			dirty:  true,
		}}, b.Blocks...)
		return
	}
	bl := &b.Blocks[idx]
	if bl.Parsed == nil {
		bl.Parsed = &H1Parsed{}
	}
	p := bl.Parsed.(*H1Parsed)
	p.Title = title
	bl.dirty = true
}
