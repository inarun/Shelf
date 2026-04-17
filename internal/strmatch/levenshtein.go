package strmatch

// Distance returns the Levenshtein edit distance between a and b, operating
// on runes rather than bytes (so "café" and "cafe" differ by the one-rune
// edit we expect, not the two-byte difference between the UTF-8 encodings).
// Memory is O(min(len(a), len(b))) — classic two-row DP.
func Distance(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	// Ensure ar is the shorter to minimize the DP row.
	if len(ar) > len(br) {
		ar, br = br, ar
	}
	if len(ar) == 0 {
		return len(br)
	}

	prev := make([]int, len(ar)+1)
	curr := make([]int, len(ar)+1)
	for i := range prev {
		prev[i] = i
	}
	for j := 1; j <= len(br); j++ {
		curr[0] = j
		for i := 1; i <= len(ar); i++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			del := prev[i] + 1
			ins := curr[i-1] + 1
			sub := prev[i-1] + cost
			curr[i] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(ar)]
}

// Ratio returns 1 - Distance(a, b) / max(len(a), len(b)) as a float in
// [0, 1]. 1.0 means identical strings (or both empty); 0.0 means no
// overlap. Rune-based, mirroring Distance.
func Ratio(a, b string) float64 {
	ar := []rune(a)
	br := []rune(b)
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	if maxLen == 0 {
		return 1.0
	}
	d := Distance(a, b)
	return 1.0 - float64(d)/float64(maxLen)
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
