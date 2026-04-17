package frontmatter

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Frontmatter is a mutable wrapper around a *yaml.Node mapping. Mutation
// preserves field order, comments, and quoting style — the whole point of
// working at the node level rather than decoding into a struct.
//
// The mutated set tracks which keys have been touched via setters since
// the Frontmatter was parsed or constructed. SaveFrontmatter in
// internal/vault/note uses it to replay only the app's changes onto a
// freshly-read disk copy, so concurrent Obsidian edits to untouched
// fields survive the write.
type Frontmatter struct {
	root    *yaml.Node // MappingNode holding the frontmatter fields
	useCRLF bool       // true if the original frontmatter block used CRLF
	mutated map[string]struct{}
}

// ErrNoFrontmatter is returned by Parse when the input has no YAML block
// delimited by "---". Callers should treat this as an "empty state" and
// either reject the file or create a new Frontmatter from scratch.
var ErrNoFrontmatter = errors.New("no YAML frontmatter delimited by ---")

// delimiter is the Markdown frontmatter fence.
const delimiter = "---"

// Parse extracts the frontmatter and body from a Markdown file. The body
// is returned verbatim (byte-for-byte identical to the input region after
// the closing delimiter); the frontmatter is available for mutation
// through the accessors in fields.go.
//
// Line endings in the frontmatter block are detected (LF vs CRLF) and
// re-applied on Serialize so round-tripping doesn't churn them.
func Parse(markdown []byte) (*Frontmatter, []byte, error) {
	// Accept "---\n" or "---\r\n" as opening delimiter.
	var openLen int
	useCRLF := false
	switch {
	case bytes.HasPrefix(markdown, []byte(delimiter+"\r\n")):
		openLen = len(delimiter) + 2
		useCRLF = true
	case bytes.HasPrefix(markdown, []byte(delimiter+"\n")):
		openLen = len(delimiter) + 1
	default:
		return nil, markdown, ErrNoFrontmatter
	}

	// Find closing "\n---\n" or "\r\n---\r\n" (or EOF variants). We search
	// for "\n---" followed by either \n, \r\n, or end-of-input.
	rest := markdown[openLen:]
	closeIdx, closeLen := findClosingDelimiter(rest)
	if closeIdx < 0 {
		return nil, markdown, fmt.Errorf("frontmatter: no closing %q delimiter", delimiter)
	}

	yamlBytes := rest[:closeIdx]
	body := rest[closeIdx+closeLen:]

	var doc yaml.Node
	// If the YAML block is empty (---\n---\n), Unmarshal returns no error
	// and doc has zero Content. Handle that as an empty mapping.
	if len(bytes.TrimSpace(yamlBytes)) > 0 {
		if err := yaml.Unmarshal(yamlBytes, &doc); err != nil {
			return nil, markdown, fmt.Errorf("frontmatter: parse YAML: %w", err)
		}
	}

	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		if doc.Content[0].Kind != yaml.MappingNode {
			return nil, markdown, fmt.Errorf("frontmatter: expected YAML mapping, got kind %d", doc.Content[0].Kind)
		}
		mapping = doc.Content[0]
	}

	return &Frontmatter{root: mapping, useCRLF: useCRLF}, body, nil
}

// findClosingDelimiter returns the byte index of the closing "---" line
// relative to rest, plus the length to skip (the "---" plus its line
// terminator). If no valid closing delimiter is present, returns -1.
//
// A closing delimiter is any "---" that is:
//   - at the start of rest (empty YAML block), or
//   - preceded by a line ending (\n or \r\n), AND
//   - followed by a line ending or end-of-input.
func findClosingDelimiter(rest []byte) (int, int) {
	search := 0
	for {
		idx := bytes.Index(rest[search:], []byte(delimiter))
		if idx < 0 {
			return -1, 0
		}
		pos := search + idx

		// Must be at line start.
		atLineStart := pos == 0 ||
			(pos >= 1 && rest[pos-1] == '\n')
		if !atLineStart {
			search = pos + 1
			continue
		}

		after := pos + len(delimiter)
		// Valid closers: EOF, \n, or \r\n after the "---".
		switch {
		case after == len(rest):
			return pos, len(delimiter)
		case after+1 <= len(rest) && rest[after] == '\n':
			return pos, len(delimiter) + 1
		case after+2 <= len(rest) && rest[after] == '\r' && rest[after+1] == '\n':
			return pos, len(delimiter) + 2
		default:
			// "---xyz" — not a closer; keep scanning.
			search = pos + 1
		}
	}
}

// NewEmpty returns a Frontmatter with no fields. Use this when the caller
// wants to construct a new book note from scratch.
func NewEmpty() *Frontmatter {
	return &Frontmatter{
		root: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"},
	}
}
