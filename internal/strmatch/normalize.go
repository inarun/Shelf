package strmatch

import (
	"strings"
	"unicode"
)

// diacriticMap folds common Latin-1 diacritics onto their ASCII bases.
// Not exhaustive — covers the cases that show up in English-language
// library metadata (Brontë, Pérez-Reverte, Solzhenitsyn transliterated
// etc.). Uppercase forms are handled after strings.ToLower so the map
// only needs lowercase entries.
var diacriticMap = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'ā': 'a',
	'ç': 'c', 'č': 'c', 'ć': 'c',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i',
	'ñ': 'n',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o', 'ō': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ū': 'u',
	'ý': 'y', 'ÿ': 'y',
	'š': 's', 'ś': 's',
	'ž': 'z', 'ź': 'z', 'ż': 'z',
	'ł': 'l',
}

// Normalize returns a lowercased, diacritic-folded, punctuation-stripped,
// whitespace-collapsed form of s. Intended for fuzzy matching of
// user-facing strings (book titles, author names). Runs of non-letter,
// non-digit runes become a single space; repeated spaces collapse.
//
// Empty input returns empty.
func Normalize(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(lower))
	for _, r := range lower {
		if m, ok := diacriticMap[r]; ok {
			b.WriteRune(m)
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Surname returns the last whitespace-separated token of Normalize(s).
// Intended for matching author names where title-case, middle initials,
// and punctuation vary. "Brandon Sanderson" → "sanderson"; "J.R.R.
// Tolkien" → "tolkien"; empty input returns "".
func Surname(s string) string {
	n := Normalize(s)
	if n == "" {
		return ""
	}
	tokens := strings.Fields(n)
	return tokens[len(tokens)-1]
}
