package paths

import "testing"

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Hyperion", "Hyperion"},
		{"colon", "Dune: Messiah", "Dune\uA789 Messiah"},
		{"question mark", "Who Goes There?", "Who Goes There\uFF1F"},
		{"double quotes", `The "Good" Book`, "The \uFF02Good\uFF02 Book"},
		{"angle brackets", "<Important>", "\uFF1CImportant\uFF1E"},
		{"pipe", "Foo | Bar", "Foo \uFF5C Bar"},
		{"asterisk", "Footnote*", "Footnote\uFF0A"},
		{"forward slash", "AC/DC", "AC\u2044DC"},
		{"backslash", `AC\DC`, "AC\u29F5DC"},
		{"multiple specials", `The "Right?" Answer: Yes*`, "The \uFF02Right\uFF1F\uFF02 Answer\uFF01 Yes\uFF0A"},
		{"null byte dropped", "Hyper\x00ion", "Hyperion"},
		{"trailing spaces trimmed", "Hyperion   ", "Hyperion"},
		{"leading spaces trimmed", "   Hyperion", "Hyperion"},
		{"multiple spaces collapsed", "A   B\t\tC", "A B C"},
		{"newline collapsed to space", "Title\nWith Break", "Title With Break"},
		{"trailing dot trimmed", "Ender.", "Ender"},
		{"trailing dots and spaces trimmed", "Ender. . ", "Ender"},
		{"empty stays empty", "", ""},
		{"only whitespace becomes empty", "   \t\n   ", ""},
		{"unicode preserved", "Café Müller", "Café Müller"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SanitizeFilename(c.in)
			// The multiple-specials case uses wrong codepoint in the expected
			// string (! instead of :) — fix up here for clarity.
			if c.name == "multiple specials" {
				c.want = "The \uFF02Right\uFF1F\uFF02 Answer\uA789 Yes\uFF0A"
			}
			if got != c.want {
				t.Errorf("SanitizeFilename(%q):\n got  %q\n want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSanitizeFilename_AllReservedAreReplaced(t *testing.T) {
	// After sanitization, no Windows-reserved char may remain.
	reserved := []rune{'<', '>', ':', '"', '|', '?', '*', '/', '\\'}
	in := "Mixed< >:\"|?*/\\ test"
	out := SanitizeFilename(in)
	for _, r := range reserved {
		for _, got := range out {
			if got == r {
				t.Errorf("SanitizeFilename left reserved char %q in %q", r, out)
			}
		}
	}
}
