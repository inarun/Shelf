package strmatch

import (
	"math"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Hyperion", "hyperion"},
		{"The Lord of the Rings", "the lord of the rings"},
		{"The Lord of the Rings  ", "the lord of the rings"},
		{"Hyperion (Hyperion Cantos, #1)", "hyperion hyperion cantos 1"},
		{"J.R.R. Tolkien", "j r r tolkien"},
		{"Brontë", "bronte"},
		{"Pérez-Reverte", "perez reverte"},
		{"Solzhenitsyn\t\nBook", "solzhenitsyn book"},
		{"A\tB   C", "a b c"},
		{"!!!punctuation???", "punctuation"},
		{"Mixed 123 digits", "mixed 123 digits"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := Normalize(c.in)
			if got != c.want {
				t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSurname(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Brandon Sanderson", "sanderson"},
		{"J.R.R. Tolkien", "tolkien"},
		{"Octavia E. Butler", "butler"},
		{"Brontë", "bronte"},
		{"  ", ""},
		{"Madonna", "madonna"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := Surname(c.in)
			if got != c.want {
				t.Errorf("Surname(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"hyperion", "hyperian", 1},
		{"café", "cafe", 1},
		{"abc", "abc", 0},
	}
	for _, c := range cases {
		t.Run(c.a+"_vs_"+c.b, func(t *testing.T) {
			if got := Distance(c.a, c.b); got != c.want {
				t.Errorf("Distance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestRatio(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"", "", 1.0},
		{"abc", "abc", 1.0},
		{"abc", "abd", 2.0 / 3.0},
		{"hyperion", "hyperian", 7.0 / 8.0},
	}
	for _, c := range cases {
		t.Run(c.a+"_vs_"+c.b, func(t *testing.T) {
			got := Ratio(c.a, c.b)
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("Ratio(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestRatio_DisjointInputsBelowHalf(t *testing.T) {
	// The exact ratio depends on incidental character overlap; the property
	// we actually care about is that very different strings land below any
	// reasonable fuzzy-match threshold.
	got := Ratio("short", "completelyunrelated")
	if got > 0.5 {
		t.Errorf("disjoint inputs should have ratio < 0.5, got %v", got)
	}
}

func TestRatio_BothEmpty(t *testing.T) {
	if Ratio("", "") != 1.0 {
		t.Error("empty vs empty should have ratio 1.0")
	}
}
