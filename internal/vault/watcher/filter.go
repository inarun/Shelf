package watcher

import (
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

// tempFileRe matches the exact shape emitted by internal/vault/atomic:
// a leading dot, the target basename, a 16-character hex random suffix,
// and the .shelf.tmp extension. Anchored on both ends so lookalikes
// can't sneak through.
var tempFileRe = regexp.MustCompile(`^\.(.+)\.([0-9a-f]{16})\.shelf\.tmp$`)

// isRelevant reports whether the watcher should emit for this basename.
// Must be a .md file, must not be a Shelf atomic-write temp file, must
// not be a dotfile (covers .obsidian/, editor backups, etc.).
func isRelevant(base string) bool {
	if base == "" {
		return false
	}
	if strings.HasPrefix(base, ".") {
		return false
	}
	if !strings.EqualFold(filepath.Ext(base), ".md") {
		return false
	}
	return true
}

// isShelfTempFile reports whether base matches the exact format emitted
// by internal/vault/atomic.Write. Verifies the hex group via
// hex.DecodeString so a user-created lookalike won't be wrongly
// suppressed.
func isShelfTempFile(base string) bool {
	m := tempFileRe.FindStringSubmatch(base)
	if m == nil {
		return false
	}
	if _, err := hex.DecodeString(m[2]); err != nil {
		return false
	}
	return true
}
