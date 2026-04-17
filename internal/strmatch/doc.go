// Package strmatch provides string normalization and edit-distance
// helpers used by the Goodreads importer's fuzzy match resolver.
//
// The normalization is intentionally ASCII-focused: lowercase,
// punctuation stripped, common Latin-1 diacritics mapped to their ASCII
// equivalents, whitespace collapsed. Full Unicode NFD decomposition
// would require golang.org/x/text, a new dependency ruled out by
// SKILL.md §Dependency policy. The current behavior is good enough for
// an English-language library with occasional European-diacritic names;
// callers that need stricter normalization should run their inputs
// through a Unicode-aware normalizer beforehand.
//
// Distance is a rune-correct Levenshtein implementation (two-row DP,
// O(min(len(a), len(b))) memory). Ratio returns 1 - Distance / max(len)
// so callers can compare against a threshold.
package strmatch
