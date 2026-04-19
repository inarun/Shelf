package handlers

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Validation limits and errors used at the API boundary. They are the
// single choke point — business-layer setters re-validate for
// defense-in-depth but handlers must catch hostile input first.

const (
	// MaxReviewBytes caps the review textarea at 64 KiB. The biggest
	// real-world reviews we know about are a few KB; 64 KiB is generous.
	MaxReviewBytes = 64 * 1024
	// MaxCSVBytes caps the Goodreads upload body. Nusayb's 123-book
	// export is ~40 KiB; 16 MiB leaves plenty of headroom for pathological
	// reviews while capping memory consumption.
	MaxCSVBytes = 16 * 1024 * 1024
	// FilenameExt is required on every book filename.
	FilenameExt = ".md"
)

var (
	ErrRatingOverallRange    = errors.New("rating.overall must be between 0 and 10")
	ErrRatingAxisNegative    = errors.New("rating axis values must be non-negative")
	ErrStatusInvalid         = errors.New("status must be one of: unread, reading, paused, finished, dnf")
	ErrStatusClobbersHistory = errors.New("refusing to set status back to 'unread' — would destroy read history")
	ErrReviewTooLarge        = fmt.Errorf("review exceeds %d bytes", MaxReviewBytes)
	ErrReviewInvalidUTF8     = errors.New("review is not valid UTF-8")
	ErrReviewNullByte        = errors.New("review contains a null byte")
	ErrFilenameEmpty         = errors.New("filename is empty")
	ErrFilenameTraversal     = errors.New("filename must not contain path separators or parent references")
	ErrFilenameExtension     = fmt.Errorf("filename must end in %s", FilenameExt)
)

// ValidateRating allows nil (clears the rating). For non-nil Rating,
// axis values must be non-negative and overall (when set) must be in
// 0..10. Unknown axis keys are rejected earlier at JSON-decode time.
func ValidateRating(r *frontmatter.Rating) error {
	if r == nil {
		return nil
	}
	for _, v := range r.TrialSystem {
		if v < 0 {
			return ErrRatingAxisNegative
		}
	}
	if r.Overall != nil {
		if *r.Overall < 0 || *r.Overall > 10 {
			return ErrRatingOverallRange
		}
	}
	return nil
}

// AllowedStatuses is the canonical enum per SKILL.md §Frontmatter schema.
var AllowedStatuses = []string{"unread", "reading", "paused", "finished", "dnf"}

// ValidateStatus rejects anything outside the SKILL.md enum.
func ValidateStatus(s string) error {
	for _, v := range AllowedStatuses {
		if s == v {
			return nil
		}
	}
	return ErrStatusInvalid
}

// ValidateStatusTransition enforces the only hard rule from SKILL.md
// §Frontmatter schema state machine: "Any state → unread is forbidden
// without explicit user confirmation (destroys read history)." Other
// transitions are permitted; side-effects are applied by the handler.
// Identity (from == to) is allowed so an idempotent PATCH doesn't fail.
func ValidateStatusTransition(from, to string) error {
	if from == to {
		return nil
	}
	if to == "unread" {
		return ErrStatusClobbersHistory
	}
	return nil
}

// ValidateReview enforces size, UTF-8, and NUL-byte checks. The text is
// treated as opaque otherwise — the body serializer handles structural
// quoting so even a stray "## " cannot start a new section.
func ValidateReview(s string) error {
	if len(s) > MaxReviewBytes {
		return ErrReviewTooLarge
	}
	if !utf8.ValidString(s) {
		return ErrReviewInvalidUTF8
	}
	if strings.ContainsRune(s, 0) {
		return ErrReviewNullByte
	}
	return nil
}

// DecodeAndValidateFilename resolves the {filename} URL path segment
// into (absolute path under booksAbs, basename). URL-decodes, rejects
// traversal, requires the .md extension, and runs the whole thing
// through paths.ValidateWithinRoot — the single FS choke point.
//
// booksAbs must be an absolute path to an existing directory.
func DecodeAndValidateFilename(booksAbs, raw string) (string, string, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", fmt.Errorf("urldecode: %w", err)
	}
	if decoded == "" {
		return "", "", ErrFilenameEmpty
	}
	if strings.ContainsAny(decoded, `/\`) || decoded == "." || decoded == ".." ||
		strings.Contains(decoded, "\x00") {
		return "", "", ErrFilenameTraversal
	}
	if !strings.HasSuffix(strings.ToLower(decoded), FilenameExt) {
		return "", "", ErrFilenameExtension
	}
	abs, err := paths.ValidateWithinRoot(booksAbs, filepath.Join(booksAbs, decoded))
	if err != nil {
		return "", "", err
	}
	return abs, decoded, nil
}
