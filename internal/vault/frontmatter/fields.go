package frontmatter

import (
	"fmt"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Field keys — centralized to catch typos at compile time.
const (
	KeyTag         = "tag"
	KeyTitle       = "title"
	KeySubtitle    = "subtitle"
	KeyAuthors     = "authors"
	KeyCategories  = "categories"
	KeySeries      = "series"
	KeySeriesIndex = "series_index"
	KeyPublisher   = "publisher"
	KeyPublish     = "publish"
	KeyTotalPages  = "total_pages"
	KeyISBN        = "isbn"
	KeyCover       = "cover"
	KeyFormat      = "format"
	KeySource      = "source"
	KeyStarted     = "started"
	KeyFinished    = "finished"
	KeyRating      = "rating"
	KeyStatus      = "status"
	KeyReadCount   = "read_count"
	DateFormat     = "2006-01-02"
)

// Valid status values per SKILL.md §Frontmatter schema.
var validStatus = map[string]bool{
	"unread":   true,
	"reading":  true,
	"paused":   true,
	"finished": true,
	"dnf":      true,
}

// Valid format values per SKILL.md §Frontmatter schema. Empty string is
// allowed — the schema uses null/absent to mean "unspecified".
var validFormat = map[string]bool{
	"":          true,
	"audiobook": true,
	"ebook":     true,
	"physical":  true,
}

// findValue returns the value node for key, or nil if not present.
// MappingNode.Content is a flat [k1, v1, k2, v2, ...] slice.
func (f *Frontmatter) findValue(key string) *yaml.Node {
	for i := 0; i+1 < len(f.root.Content); i += 2 {
		if f.root.Content[i].Value == key {
			return f.root.Content[i+1]
		}
	}
	return nil
}

// setValue replaces the value node for key in place, or appends
// [key, value] if key is absent. Existing field order is preserved.
// Records the key in the mutated set so SaveFrontmatter knows which
// fields to carry onto a fresh disk read.
func (f *Frontmatter) setValue(key string, value *yaml.Node) {
	f.markMutated(key)
	for i := 0; i+1 < len(f.root.Content); i += 2 {
		if f.root.Content[i].Value == key {
			f.root.Content[i+1] = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	f.root.Content = append(f.root.Content, keyNode, value)
}

func (f *Frontmatter) markMutated(key string) {
	if f.mutated == nil {
		f.mutated = map[string]struct{}{}
	}
	f.mutated[key] = struct{}{}
}

// MutatedKeys returns the frontmatter keys that have been written
// through setters since parse/construction, in no particular order.
// Callers doing a frontmatter-only write use this to replay mutations
// onto a freshly-read disk copy without clobbering untouched fields.
func (f *Frontmatter) MutatedKeys() []string {
	out := make([]string, 0, len(f.mutated))
	for k := range f.mutated {
		out = append(out, k)
	}
	return out
}

// GetRawValue returns the value node for key, or nil if absent. Used by
// callers that need to copy mutated fields across Frontmatter instances
// without going through the typed accessors.
func (f *Frontmatter) GetRawValue(key string) *yaml.Node {
	return f.findValue(key)
}

// SetRawValue installs value under key, marking it mutated. Intended
// for cross-Frontmatter copying — the typed setters are preferred for
// anything the caller produces itself.
func (f *Frontmatter) SetRawValue(key string, value *yaml.Node) {
	f.setValue(key, value)
}

func (f *Frontmatter) getString(key string) string {
	v := f.findValue(key)
	if v == nil {
		return ""
	}
	// A null-tagged scalar (written by a clear operation) should read back
	// as empty, not the literal word "null".
	if v.Tag == "!!null" {
		return ""
	}
	return v.Value
}

func (f *Frontmatter) getStringArray(key string) []string {
	v := f.findValue(key)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(v.Content))
	for _, item := range v.Content {
		out = append(out, item.Value)
	}
	return out
}

func (f *Frontmatter) getInt(key string) *int {
	v := f.findValue(key)
	if v == nil || v.Kind != yaml.ScalarNode || v.Value == "" || v.Tag == "!!null" {
		return nil
	}
	n, err := strconv.Atoi(v.Value)
	if err != nil {
		return nil
	}
	return &n
}

// getFloat returns a parsed float64 pointer, or nil when the field is
// absent, empty, null, or not parseable. Tolerates integer forms too so
// series_index values like "1" and "1.5" both round-trip cleanly.
func (f *Frontmatter) getFloat(key string) *float64 {
	v := f.findValue(key)
	if v == nil || v.Kind != yaml.ScalarNode || v.Value == "" || v.Tag == "!!null" {
		return nil
	}
	n, err := strconv.ParseFloat(v.Value, 64)
	if err != nil {
		return nil
	}
	return &n
}

func (f *Frontmatter) getDateArray(key string) []time.Time {
	v := f.findValue(key)
	if v == nil || v.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]time.Time, 0, len(v.Content))
	for _, item := range v.Content {
		t, err := time.Parse(DateFormat, item.Value)
		if err != nil {
			// Malformed date — skip, don't fail the whole accessor. The
			// domain layer runs its own validator that surfaces this.
			continue
		}
		out = append(out, t)
	}
	return out
}

// Getters — stable string accessors.

func (f *Frontmatter) Tag() string        { return f.getString(KeyTag) }
func (f *Frontmatter) Title() string      { return f.getString(KeyTitle) }
func (f *Frontmatter) Subtitle() string   { return f.getString(KeySubtitle) }
func (f *Frontmatter) Publisher() string  { return f.getString(KeyPublisher) }
func (f *Frontmatter) Publish() string    { return f.getString(KeyPublish) }
func (f *Frontmatter) ISBN() string       { return f.getString(KeyISBN) }
func (f *Frontmatter) Cover() string      { return f.getString(KeyCover) }
func (f *Frontmatter) Format() string     { return f.getString(KeyFormat) }
func (f *Frontmatter) Source() string     { return f.getString(KeySource) }
func (f *Frontmatter) Status() string     { return f.getString(KeyStatus) }
func (f *Frontmatter) Authors() []string  { return f.getStringArray(KeyAuthors) }
func (f *Frontmatter) Categories() []string { return f.getStringArray(KeyCategories) }
func (f *Frontmatter) Series() string     { return f.getString(KeySeries) }

// SeriesIndex returns the series position as a pointer (nil when absent).
// Value is a float to support fractional positions like 1.5. For whole
// numbers both "1" and "1.0" in the source parse to 1.0.
func (f *Frontmatter) SeriesIndex() *float64 { return f.getFloat(KeySeriesIndex) }

// TotalPages returns nil when the field is absent or empty. Use a pointer
// because "not set" is semantically distinct from 0.
func (f *Frontmatter) TotalPages() *int { return f.getInt(KeyTotalPages) }

// ReadCount defaults to 0 when absent.
func (f *Frontmatter) ReadCount() int {
	if p := f.getInt(KeyReadCount); p != nil {
		return *p
	}
	return 0
}

// Started returns ISO-parsed start dates. Malformed entries are skipped.
func (f *Frontmatter) Started() []time.Time { return f.getDateArray(KeyStarted) }

// Finished returns ISO-parsed finish dates, paired by index with Started.
func (f *Frontmatter) Finished() []time.Time { return f.getDateArray(KeyFinished) }

// Setters — mutate the underlying node in place.

func (f *Frontmatter) SetTitle(s string) {
	f.setValue(KeyTitle, scalarString(s))
}

// SetSeries updates the series name. An empty string clears the field's
// value but leaves it present (matching how the Book Search plugin
// emits unset string fields).
func (f *Frontmatter) SetSeries(s string) {
	f.setValue(KeySeries, scalarString(s))
}

// SetSeriesIndex accepts nil to clear the index, or a value. Whole
// numbers serialize as integers ("1"), fractions as decimals ("1.5").
// Negative or NaN/Inf values are rejected.
func (f *Frontmatter) SetSeriesIndex(n *float64) error {
	if n == nil {
		f.setValue(KeySeriesIndex, nullScalar())
		return nil
	}
	if *n < 0 || *n != *n || *n*0 != 0 { // NaN: n != n; Inf: n*0 != 0
		return fmt.Errorf("series_index %v invalid", *n)
	}
	f.setValue(KeySeriesIndex, scalarFloat(*n))
	return nil
}

// SetStatus validates against the enum in SKILL.md §Frontmatter schema
// state machine. Callers that want to transition also need to update
// started/finished arrays appropriately — that's the domain/timeline
// layer's job.
func (f *Frontmatter) SetStatus(s string) error {
	if !validStatus[s] {
		return fmt.Errorf("invalid status %q (valid: unread, reading, paused, finished, dnf)", s)
	}
	f.setValue(KeyStatus, scalarString(s))
	return nil
}

// SetReadCount updates the read_count field. Callers are expected to
// keep this consistent with len(finished); the domain layer enforces
// that invariant on every state transition.
func (f *Frontmatter) SetReadCount(n int) {
	if n < 0 {
		n = 0
	}
	f.setValue(KeyReadCount, scalarInt(n))
}

// SetTag updates the tag field (e.g., "📚Book"). The Obsidian Book Search
// plugin template emits this as a string scalar, not an array.
func (f *Frontmatter) SetTag(s string) {
	f.setValue(KeyTag, scalarString(s))
}

// SetSubtitle updates the subtitle field. Empty string clears the value
// but leaves the field present.
func (f *Frontmatter) SetSubtitle(s string) {
	f.setValue(KeySubtitle, scalarString(s))
}

// SetAuthors replaces the authors array. A nil or empty slice renders as
// "[]" (flow-style) to match the template default. Each name becomes a
// string scalar in a block-style sequence so the YAML matches the Book
// Search plugin layout (one author per line).
func (f *Frontmatter) SetAuthors(names []string) {
	f.setValue(KeyAuthors, stringSequence(names))
}

// SetCategories replaces the categories array. Layout matches SetAuthors.
func (f *Frontmatter) SetCategories(names []string) {
	f.setValue(KeyCategories, stringSequence(names))
}

// SetPublisher updates the publisher field.
func (f *Frontmatter) SetPublisher(s string) {
	f.setValue(KeyPublisher, scalarString(s))
}

// SetPublish updates the publish date field. Stored as a string because
// the schema accepts either "YYYY" or "YYYY-MM-DD" — callers format
// beforehand if they want a specific precision.
func (f *Frontmatter) SetPublish(s string) {
	f.setValue(KeyPublish, scalarString(s))
}

// SetTotalPages accepts nil to clear, or a non-negative integer. Negative
// values are rejected.
func (f *Frontmatter) SetTotalPages(n *int) error {
	if n == nil {
		f.setValue(KeyTotalPages, nullScalar())
		return nil
	}
	if *n < 0 {
		return fmt.Errorf("total_pages %d must be non-negative", *n)
	}
	f.setValue(KeyTotalPages, scalarInt(*n))
	return nil
}

// SetISBN updates the isbn field. Callers are expected to have normalized
// the value (digits only for ISBN-13, digits + optional X for ISBN-10);
// this setter does not validate format.
func (f *Frontmatter) SetISBN(s string) {
	f.setValue(KeyISBN, scalarString(s))
}

// SetCover updates the cover field. Stored as a string (local cache path
// or URL per SKILL.md §Frontmatter schema).
func (f *Frontmatter) SetCover(s string) {
	f.setValue(KeyCover, scalarString(s))
}

// SetFormat restricts values to the enum in SKILL.md §Frontmatter schema
// (audiobook, ebook, physical) or empty string for "unset". Empty string
// serializes as a null scalar to match the template's unset convention.
func (f *Frontmatter) SetFormat(s string) error {
	if !validFormat[s] {
		return fmt.Errorf("invalid format %q (valid: audiobook, ebook, physical, or empty)", s)
	}
	if s == "" {
		f.setValue(KeyFormat, nullScalar())
		return nil
	}
	f.setValue(KeyFormat, scalarString(s))
	return nil
}

// SetSource updates the freeform source field (e.g., "Audible", "Libby").
func (f *Frontmatter) SetSource(s string) {
	f.setValue(KeySource, scalarString(s))
}

// AppendStarted appends a new ISO-formatted date to the started array.
// Creates the field if absent.
func (f *Frontmatter) AppendStarted(t time.Time) {
	f.appendDate(KeyStarted, t)
}

// AppendFinished appends a new ISO-formatted date to the finished array.
// Creates the field if absent. Does NOT increment ReadCount — the domain
// layer does that as a paired operation.
func (f *Frontmatter) AppendFinished(t time.Time) {
	f.appendDate(KeyFinished, t)
}

func (f *Frontmatter) appendDate(key string, t time.Time) {
	v := f.findValue(key)
	date := scalarString(t.Format(DateFormat))
	if v == nil {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		seq.Content = []*yaml.Node{date}
		f.setValue(key, seq)
		return
	}
	if v.Kind != yaml.SequenceNode {
		// Replace a non-sequence value with a fresh sequence. Losing a
		// non-array value is acceptable here — the schema says these are
		// arrays, and anything else is malformed input.
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		seq.Content = []*yaml.Node{date}
		f.setValue(key, seq)
		return
	}
	v.Content = append(v.Content, date)
	f.markMutated(key)
}

// Constructors for YAML scalar nodes. Centralized so node tags/styles
// stay consistent across setters.

func scalarString(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func scalarInt(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}
}

// scalarFloat emits whole numbers with an "!!int" tag ("1") and fractional
// values with "!!float" ("1.5"), using the shortest round-trip form. This
// matches hand-written YAML where series_index: 1 reads naturally.
func scalarFloat(n float64) *yaml.Node {
	if n == float64(int64(n)) {
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!int",
			Value: strconv.FormatInt(int64(n), 10),
		}
	}
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!float",
		Value: strconv.FormatFloat(n, 'f', -1, 64),
	}
}

func nullScalar() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
}

// stringSequence builds a YAML sequence of string scalars. Empty input
// renders as a flow-style empty sequence ("[]"), matching the Book Search
// plugin template's convention; non-empty input uses block style so each
// item lands on its own line.
func stringSequence(values []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if len(values) == 0 {
		seq.Style = yaml.FlowStyle
		return seq
	}
	for _, v := range values {
		seq.Content = append(seq.Content, scalarString(v))
	}
	return seq
}
