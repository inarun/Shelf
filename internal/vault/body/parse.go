package body

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parse splits body into a slice of Blocks at ##-heading boundaries.
//
// The leading region (everything before the first ##) is parsed as an
// H1 block when an H1 heading is present, otherwise as an unknown block
// so its raw bytes survive round-trip. Each subsequent ##-section is
// classified by heading text; unrecognized sections stay as KindUnknown
// with Parsed=nil and Raw holding the verbatim source.
//
// Parse never returns an error for well-formed or malformed Markdown —
// it always returns a *Body whose concatenated block Raw values equal
// the input byte-for-byte. Callers that care about structure inspect
// Parsed fields on each block.
func Parse(body []byte) (*Body, error) {
	regions := splitAtH2(body)
	out := &Body{}
	for i, r := range regions {
		if len(r) == 0 {
			// An empty pre-H2 region is normal when body starts with a
			// ##-section directly. Skip it.
			if i == 0 {
				continue
			}
			// A subsequent empty region would imply two consecutive H2
			// splits with no bytes between — can't happen because each
			// split keeps the heading line with its region.
			continue
		}
		if i == 0 {
			out.Blocks = append(out.Blocks, parsePreH2(r))
			continue
		}
		out.Blocks = append(out.Blocks, parseH2Region(r))
	}
	return out, nil
}

// splitAtH2 partitions body so region[0] is the bytes before the first
// ##-heading and region[i>0] starts exactly at a ##-heading line and
// runs up to (but not including) the next ##-heading or end of input.
// Concatenating the regions reproduces body byte-for-byte.
func splitAtH2(body []byte) [][]byte {
	var regions [][]byte
	prev := 0
	i := 0
	atLineStart := true
	for i < len(body) {
		if atLineStart && isH2Start(body[i:]) {
			if i > prev || len(regions) == 0 {
				regions = append(regions, body[prev:i])
			}
			prev = i
		}
		if body[i] == '\n' {
			atLineStart = true
		} else {
			atLineStart = false
		}
		i++
	}
	if prev < len(body) || len(regions) == 0 {
		regions = append(regions, body[prev:])
	}
	return regions
}

// isH2Start reports whether the byte slice begins with a literal "## "
// at column zero. Headings without a trailing space ("##foo") are not
// headings per CommonMark — we match the same rule.
func isH2Start(b []byte) bool {
	return bytes.HasPrefix(b, []byte("## "))
}

// headingTextH2 extracts the trimmed heading label from a ##-heading
// line. Assumes the region begins with "## ". Consumes only the first
// line of the region.
func headingTextH2(region []byte) string {
	first := region
	if nl := bytes.IndexByte(region, '\n'); nl >= 0 {
		first = region[:nl]
	}
	// Strip CR if the file uses CRLF.
	first = bytes.TrimSuffix(first, []byte("\r"))
	return strings.TrimSpace(strings.TrimPrefix(string(first), "##"))
}

// parseH2Region classifies a region starting with "## " and fills in
// its Parsed view. The region's Raw always equals the input slice.
func parseH2Region(region []byte) Block {
	heading := headingTextH2(region)
	// The Rating section carries a computed-overall suffix in its
	// heading ("## Rating — ★ 6/5"), so a literal map lookup won't
	// match. Detect the prefix explicitly.
	if isRatingHeading(heading) {
		return Block{Kind: KindRating, Raw: region, Parsed: parseRatingSection(region)}
	}
	kind, ok := recognizedH2[heading]
	if !ok {
		return Block{Kind: KindUnknown, Raw: region}
	}
	switch kind {
	case KindNotes, KindQuotes:
		return Block{Kind: kind, Raw: region, Parsed: parseTextSection(region)}
	case KindKeyIdeas, KindActions, KindRelated:
		return Block{Kind: kind, Raw: region, Parsed: parseListSection(region)}
	case KindTimeline:
		return Block{Kind: kind, Raw: region, Parsed: parseTimelineSection(region)}
	}
	return Block{Kind: kind, Raw: region}
}

// isRatingHeading matches "Rating" optionally followed by a dash and
// overall-score suffix. Matches "Rating", "Rating — ★ 6/5", "Rating - 4/5".
// Whitespace-tolerant. Plural / "Ratings notes" headings are NOT
// considered matches.
func isRatingHeading(h string) bool {
	h = strings.TrimSpace(h)
	if h == "Rating" {
		return true
	}
	rest, ok := stripPrefix(h, "Rating")
	if !ok {
		return false
	}
	rest = strings.TrimLeft(rest, " \t")
	// Must start with an em-dash, en-dash, or ASCII hyphen separator.
	r, w := firstRune(rest)
	if r != '\u2014' && r != '\u2013' && r != '-' {
		return false
	}
	_ = w
	return true
}

func stripPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return s, false
	}
	return s[len(prefix):], true
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

// bodyAfterHeading returns the region's bytes after the first line
// (the ##-heading), which is the section content. One leading blank
// line is removed when present so callers don't carry it in their
// parsed view; the serializer reinserts it.
func bodyAfterHeading(region []byte) []byte {
	nl := bytes.IndexByte(region, '\n')
	if nl < 0 {
		return nil
	}
	rest := region[nl+1:]
	switch {
	case bytes.HasPrefix(rest, []byte("\r\n")):
		return rest[2:]
	case bytes.HasPrefix(rest, []byte("\n")):
		return rest[1:]
	}
	return rest
}

func parseTextSection(region []byte) *TextParsed {
	return &TextParsed{Text: bodyAfterHeading(region)}
}

// parseListSection splits the content after the heading into one entry
// per line. Lines that are blank (only whitespace) are skipped from the
// Items list; they're preserved in Raw for round-trip but canonical
// regeneration writes a single blank line between heading and items.
func parseListSection(region []byte) *ListParsed {
	content := bodyAfterHeading(region)
	out := &ListParsed{}
	for _, line := range splitLinesKeepEOL(content) {
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(bytes.TrimSpace(trimmed)) == 0 {
			continue
		}
		out.Items = append(out.Items, append([]byte(nil), trimmed...))
	}
	return out
}

// timelineLineRe matches "- YYYY-MM-DD — <text>" using either the
// en-dash U+2013 or em-dash U+2014 as the separator. The raw spec uses
// em-dash; tolerating en-dash costs nothing and helps users who type
// the wrong one.
var timelineLineRe = regexp.MustCompile(`^-\s+(\d{4}-\d{2}-\d{2})\s*[\x{2013}\x{2014}]\s*(.+)$`)

func parseTimelineSection(region []byte) *TimelineParsed {
	content := bodyAfterHeading(region)
	out := &TimelineParsed{}
	for _, line := range splitLinesKeepEOL(content) {
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(bytes.TrimSpace(trimmed)) == 0 {
			continue
		}
		rawCopy := append([]byte(nil), line...)
		m := timelineLineRe.FindStringSubmatch(string(trimmed))
		if m == nil {
			out.Events = append(out.Events, TimelineEvent{Raw: rawCopy})
			continue
		}
		date, err := time.Parse("2006-01-02", m[1])
		if err != nil {
			out.Events = append(out.Events, TimelineEvent{Raw: rawCopy})
			continue
		}
		out.Events = append(out.Events, TimelineEvent{
			Date: date,
			Text: strings.TrimSpace(m[2]),
			Raw:  rawCopy,
		})
	}
	return out
}

// parsePreH2 handles the H1 region: optional H1 heading + optional
// Rating line + prose. If no H1 line is present the region becomes a
// KindUnknown block so its content still round-trips.
func parsePreH2(region []byte) Block {
	if !hasH1Line(region) {
		return Block{Kind: KindUnknown, Raw: region}
	}
	p := &H1Parsed{}
	for _, line := range splitLinesKeepEOL(region) {
		trimmed := bytes.TrimRight(line, "\r\n")
		switch {
		case bytes.HasPrefix(trimmed, []byte("# ")):
			p.Title = strings.TrimSpace(strings.TrimPrefix(string(trimmed), "# "))
		case len(p.Title) > 0 && p.Rating == nil:
			if r, ok := parseRatingLine(trimmed); ok {
				rr := r
				p.Rating = &rr
			}
		}
	}
	return Block{Kind: KindH1, Raw: region, Parsed: p}
}

func hasH1Line(region []byte) bool {
	for _, line := range splitLinesKeepEOL(region) {
		trimmed := bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(trimmed, []byte("# ")) {
			return true
		}
	}
	return false
}

// ratingLineRe matches "Rating — N/5" with em-dash (tolerating en-dash
// for the same reason as timeline lines). Rating must be a single digit
// 1..5 per SKILL.md §Frontmatter schema; 0 means unrated and should not
// appear as a body line.
var ratingLineRe = regexp.MustCompile(`^Rating\s*[\x{2013}\x{2014}]\s*([1-5])/5\s*$`)

func parseRatingLine(line []byte) (int, bool) {
	m := ratingLineRe.FindStringSubmatch(string(line))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// splitLinesKeepEOL returns each line including its terminator. Trailing
// content without a newline is returned as a final element without one.
// An empty input returns nil.
func splitLinesKeepEOL(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	var out [][]byte
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			out = append(out, b[start:i+1])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
