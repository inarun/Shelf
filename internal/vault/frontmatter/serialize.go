package frontmatter

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Serialize emits the frontmatter followed by body verbatim.
//
// Key guarantee: for fields the app hasn't touched, the YAML output is
// byte-equivalent to the original — order, comments, quoting style, and
// aliases are all preserved because serialization goes through yaml.Node,
// not a high-level struct. Fields the app has set or appended get
// canonical YAML formatting at their existing position (or appended).
//
// Line endings in the frontmatter block match what Parse saw on input
// (LF stays LF, CRLF stays CRLF). Body bytes are appended without any
// line-ending normalization.
func (f *Frontmatter) Serialize(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(delimiter)
	buf.WriteByte('\n')

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f.root); err != nil {
		return nil, fmt.Errorf("frontmatter: encode YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("frontmatter: close encoder: %w", err)
	}

	buf.WriteString(delimiter)
	buf.WriteByte('\n')

	out := buf.Bytes()
	if f.useCRLF {
		// Normalize any stray CRLF first, then convert every LF to CRLF
		// so body preservation is unaffected.
		out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}

	return append(out, body...), nil
}
