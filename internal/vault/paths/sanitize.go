package paths

import "strings"

// SanitizeFilename converts the user-facing string s into a form suitable
// for use inside a Windows filename:
//
//   - Windows-reserved characters (< > : " | ? * / \) are replaced with
//     visually similar Unicode codepoints so titles remain readable.
//     The substitution table is documented in SKILL.md §Filename.
//   - Null bytes are dropped.
//   - Runs of whitespace (including tabs/newlines) collapse to single
//     spaces.
//   - Leading/trailing whitespace and trailing dots are trimmed; Windows
//     silently strips trailing dots and spaces, so keeping them would
//     create filenames that disagree with what's on disk.
//
// SanitizeFilename does not add ".md" or any separator — it operates on
// individual filename components.
func SanitizeFilename(s string) string {
	mapped := strings.Map(func(r rune) rune {
		switch r {
		case ':':
			return '\uA789' // ꞉ MODIFIER LETTER COLON
		case '?':
			return '\uFF1F' // ？ FULLWIDTH QUESTION MARK
		case '"':
			return '\uFF02' // ＂ FULLWIDTH QUOTATION MARK
		case '<':
			return '\uFF1C' // ＜ FULLWIDTH LESS-THAN SIGN
		case '>':
			return '\uFF1E' // ＞ FULLWIDTH GREATER-THAN SIGN
		case '|':
			return '\uFF5C' // ｜ FULLWIDTH VERTICAL LINE
		case '*':
			return '\uFF0A' // ＊ FULLWIDTH ASTERISK
		case '/':
			return '\u2044' // ⁄ FRACTION SLASH
		case '\\':
			return '\u29F5' // ⧵ REVERSE SOLIDUS OPERATOR
		case 0:
			return -1 // drop null bytes
		}
		return r
	}, s)

	collapsed := strings.Join(strings.Fields(mapped), " ")
	// TrimRight after collapse in case a trailing dot was the sole remainder.
	return strings.TrimRight(collapsed, ". ")
}
