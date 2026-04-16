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
func (f *Frontmatter) setValue(key string, value *yaml.Node) {
	for i := 0; i+1 < len(f.root.Content); i += 2 {
		if f.root.Content[i].Value == key {
			f.root.Content[i+1] = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	f.root.Content = append(f.root.Content, keyNode, value)
}

func (f *Frontmatter) getString(key string) string {
	v := f.findValue(key)
	if v == nil {
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

// TotalPages returns nil when the field is absent or empty. Use a pointer
// because "not set" is semantically distinct from 0.
func (f *Frontmatter) TotalPages() *int { return f.getInt(KeyTotalPages) }

// Rating returns nil when unrated; 1..5 otherwise.
func (f *Frontmatter) Rating() *int { return f.getInt(KeyRating) }

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

// SetRating accepts nil to clear the rating, or *r in the range 1..5.
// Returns an error for out-of-range values; never silently clamps.
func (f *Frontmatter) SetRating(r *int) error {
	if r == nil {
		f.setValue(KeyRating, nullScalar())
		return nil
	}
	if *r < 1 || *r > 5 {
		return fmt.Errorf("rating %d out of range 1..5", *r)
	}
	f.setValue(KeyRating, scalarInt(*r))
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
}

// Constructors for YAML scalar nodes. Centralized so node tags/styles
// stay consistent across setters.

func scalarString(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func scalarInt(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}
}

func nullScalar() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
}
