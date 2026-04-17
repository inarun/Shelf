package server

import (
	"crypto/rand"
	"fmt"
)

// Keys holds the process-lifetime secrets used by the server. The
// HMAC field signs CSRF tokens; SessionSecret is reserved for future
// signed cookies. Both are 32 bytes from crypto/rand and NEVER
// persisted to disk.
//
// Regenerating on every startup is intentional (per Session 4
// decision): it invalidates all outstanding CSRF tokens when the
// process restarts, guaranteeing the threat model doesn't silently
// broaden if the cookie outlives the server's intent.
type Keys struct {
	HMAC          [32]byte
	SessionSecret [32]byte
}

// NewKeys returns a fresh set of server keys. A crypto/rand failure
// here is unrecoverable — we refuse to start.
func NewKeys() (*Keys, error) {
	k := &Keys{}
	if _, err := rand.Read(k.HMAC[:]); err != nil {
		return nil, fmt.Errorf("server: read HMAC entropy: %w", err)
	}
	if _, err := rand.Read(k.SessionSecret[:]); err != nil {
		return nil, fmt.Errorf("server: read session entropy: %w", err)
	}
	return k, nil
}
