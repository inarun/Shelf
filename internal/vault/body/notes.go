package body

import "bytes"

// Notes returns the current text of the ## Notes section, or "" when
// the section is absent or empty. Mirrors what SetNotes stores: the
// opaque body text with a single leading blank line trimmed. Callers
// rendering a review in a <textarea> get the raw content; UI escaping
// is the template's job.
func (b *Body) Notes() string {
	idx := b.indexOf(KindNotes)
	if idx < 0 {
		return ""
	}
	p, ok := b.Blocks[idx].Parsed.(*TextParsed)
	if !ok || p == nil {
		return ""
	}
	return string(p.Text)
}

// SetNotes replaces the entire contents of the ## Notes section. Creates
// the section at its canonical position when absent. The input is stored
// verbatim in the TextParsed payload; callers that want a trailing newline
// after the text are expected to include one. The block is marked dirty
// so Serialize regenerates it.
//
// Callers composing review-style prose (e.g., the Goodreads importer)
// should prepend any provenance line and apply any per-line escaping
// before calling this helper — SetNotes is content-agnostic.
func (b *Body) SetNotes(text string) {
	b.EnsureSection(KindNotes)
	idx := b.indexOf(KindNotes)
	bl := &b.Blocks[idx]
	if bl.Parsed == nil {
		bl.Parsed = &TextParsed{}
	}
	p := bl.Parsed.(*TextParsed)
	p.Text = []byte(text)
	bl.dirty = true
}

// AppendNotes appends text to the existing ## Notes section, separated
// from prior content by a blank line. Creates the section at its
// canonical position when absent. Useful for adding an imported review
// below user-authored notes rather than replacing them.
func (b *Body) AppendNotes(text string) {
	b.EnsureSection(KindNotes)
	idx := b.indexOf(KindNotes)
	bl := &b.Blocks[idx]
	if bl.Parsed == nil {
		bl.Parsed = &TextParsed{}
	}
	p := bl.Parsed.(*TextParsed)
	if len(bytes.TrimSpace(p.Text)) == 0 {
		p.Text = []byte(text)
	} else {
		trimmed := bytes.TrimRight(p.Text, "\n")
		buf := make([]byte, 0, len(trimmed)+len(text)+2)
		buf = append(buf, trimmed...)
		buf = append(buf, '\n', '\n')
		buf = append(buf, text...)
		p.Text = buf
	}
	bl.dirty = true
}
