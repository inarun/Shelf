package handlers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

func fp(v float64) *float64 { return &v }

func TestValidateRating(t *testing.T) {
	cases := []struct {
		name string
		in   *frontmatter.Rating
		err  error
	}{
		{"nil ok", nil, nil},
		{"overall 5", &frontmatter.Rating{Overall: fp(5)}, nil},
		{"overall 10 (bumped)", &frontmatter.Rating{Overall: fp(10)}, nil},
		{"overall -1 rejected", &frontmatter.Rating{Overall: fp(-1)}, ErrRatingOverallRange},
		{"overall 11 rejected", &frontmatter.Rating{Overall: fp(11)}, ErrRatingOverallRange},
		{"all axes set", &frontmatter.Rating{TrialSystem: map[string]int{
			"emotional_impact": 5, "characters": 4, "plot": 5,
			"dialogue_prose": 3, "cinematography_worldbuilding": 5,
		}}, nil},
		{"negative axis rejected", &frontmatter.Rating{TrialSystem: map[string]int{
			"plot": -1,
		}}, ErrRatingAxisNegative},
	}
	for _, tc := range cases {
		if got := ValidateRating(tc.in); !errors.Is(got, tc.err) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.err)
		}
	}
}

func TestValidateStatus(t *testing.T) {
	for _, s := range AllowedStatuses {
		if err := ValidateStatus(s); err != nil {
			t.Errorf("%q rejected: %v", s, err)
		}
	}
	for _, bad := range []string{"", "Reading", "abandoned", "finish"} {
		if err := ValidateStatus(bad); err == nil {
			t.Errorf("%q accepted; want rejection", bad)
		}
	}
}

func TestValidateStatusTransition(t *testing.T) {
	cases := []struct {
		from, to string
		wantErr  error
	}{
		{"unread", "reading", nil},
		{"reading", "finished", nil},
		{"finished", "reading", nil}, // re-read
		{"dnf", "reading", nil},
		{"reading", "paused", nil},
		{"reading", "reading", nil}, // identity allowed
		{"reading", "unread", ErrStatusClobbersHistory},
		{"finished", "unread", ErrStatusClobbersHistory},
		{"dnf", "unread", ErrStatusClobbersHistory},
		{"unread", "unread", nil}, // identity ok even for unread
	}
	for _, tc := range cases {
		err := ValidateStatusTransition(tc.from, tc.to)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s→%s: got %v, want %v", tc.from, tc.to, err, tc.wantErr)
		}
	}
}

func TestValidateReview(t *testing.T) {
	if err := ValidateReview(""); err != nil {
		t.Errorf("empty review should be ok, got %v", err)
	}
	if err := ValidateReview("normal review text"); err != nil {
		t.Errorf("normal text rejected: %v", err)
	}
	if err := ValidateReview(strings.Repeat("a", MaxReviewBytes+1)); !errors.Is(err, ErrReviewTooLarge) {
		t.Errorf("oversized review: got %v", err)
	}
	if err := ValidateReview("with null \x00 byte"); !errors.Is(err, ErrReviewNullByte) {
		t.Errorf("null byte: got %v", err)
	}
	if err := ValidateReview("\xff\xfe"); !errors.Is(err, ErrReviewInvalidUTF8) {
		t.Errorf("invalid utf8: got %v", err)
	}
}

func TestDecodeAndValidateFilename(t *testing.T) {
	root := t.TempDir()
	// paths.ValidateWithinRoot requires the parent to exist (which root is),
	// and EvalSymlinks to resolve. Book files themselves don't need to exist.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		raw    string
		wantOk bool
	}{
		{"canonical", "Hyperion by Dan Simmons.md", true},
		{"url-encoded space", "Hyperion%20by%20Dan%20Simmons.md", true},
		{"non-ASCII ok", "Café by Marie.md", true},
		{"traversal", "../../etc/passwd", false},
		{"encoded traversal", "..%2Ffoo.md", false},
		{"slash inside", "sub/file.md", false},
		{"backslash inside", "sub\\file.md", false},
		{"empty", "", false},
		{"dot", ".", false},
		{"no md ext", "Hyperion by Dan Simmons", false},
		{"md upper ok", "Hyperion by Dan Simmons.MD", true},
		{"null byte", "bad\x00.md", false},
	}
	for _, tc := range cases {
		abs, base, err := DecodeAndValidateFilename(absRoot, tc.raw)
		if tc.wantOk {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			if base == "" {
				t.Errorf("%s: empty basename", tc.name)
			}
			// Must live under the root.
			rel, err := filepath.Rel(absRoot, abs)
			if err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("%s: resolved %q escapes root %q", tc.name, abs, absRoot)
			}
		} else {
			if err == nil {
				t.Errorf("%s: expected error, got abs=%q", tc.name, abs)
			}
		}
	}

	// Bonus: sanity-check a real file existing doesn't change behavior.
	realFile := filepath.Join(root, "Hyperion by Dan Simmons.md")
	_ = os.WriteFile(realFile, []byte(""), 0o600)
	if _, _, err := DecodeAndValidateFilename(absRoot, "Hyperion by Dan Simmons.md"); err != nil {
		t.Errorf("real file: %v", err)
	}
}
