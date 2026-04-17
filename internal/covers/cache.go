package covers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/inarun/Shelf/internal/providers/metadata"
	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// CoverRefPrefix is the URL path prefix the HTTP server serves cached
// covers under. Stored in frontmatter as "/covers/<sha256>.<ext>".
const CoverRefPrefix = "/covers/"

// ErrInvalidFilename is returned by AbsPath when the requested name
// doesn't match the sha256+ext pattern the cache produces — the HTTP
// handler converts this into a 404.
var ErrInvalidFilename = errors.New("covers: invalid filename")

// Cache is a content-addressed image cache rooted at RootAbs.
type Cache struct {
	RootAbs string
}

// New ensures root exists (0o700) and returns a Cache. Does not probe
// write permissions separately; the caller (cmd/shelf) has already
// verified data.directory is writable during config validation.
func New(rootAbs string) (*Cache, error) {
	if rootAbs == "" {
		return nil, errors.New("covers: empty root")
	}
	if err := os.MkdirAll(rootAbs, 0o700); err != nil {
		return nil, fmt.Errorf("covers: mkdir %s: %w", rootAbs, err)
	}
	return &Cache{RootAbs: rootAbs}, nil
}

// ProviderKey composes a stable cache key from a provider name and the
// provider-specific cover ref. Separating the provider name means two
// providers returning the same ref (say, "isbn:9780...") get different
// cache slots.
func ProviderKey(providerName, ref string) string {
	return strings.ToLower(providerName) + "/" + ref
}

// Store writes img to the cache under sha256(providerKey) and returns
// the cover reference suitable for storing in frontmatter.
func (c *Cache) Store(providerKey string, img *metadata.CoverImage) (coverRef string, err error) {
	if img == nil || len(img.Bytes) == 0 {
		return "", errors.New("covers: empty image")
	}
	ext := img.Ext
	if ext != ".jpg" && ext != ".png" {
		return "", fmt.Errorf("covers: unsupported ext %q", ext)
	}
	if providerKey == "" {
		return "", errors.New("covers: empty provider key")
	}
	h := sha256.Sum256([]byte(providerKey))
	name := hex.EncodeToString(h[:]) + ext
	abs, err := paths.ValidateWithinRoot(c.RootAbs, name)
	if err != nil {
		return "", fmt.Errorf("covers: validate %s: %w", name, err)
	}
	if err := atomic.Write(abs, img.Bytes, 0o600); err != nil {
		return "", fmt.Errorf("covers: write %s: %w", abs, err)
	}
	return CoverRefPrefix + name, nil
}

// Exists reports whether the given coverRef ("/covers/<hash>.<ext>")
// points to a file that's actually in the cache. Returns false for any
// ref shape that isn't one of our own.
func (c *Cache) Exists(coverRef string) bool {
	name, ok := filenameFromRef(coverRef)
	if !ok {
		return false
	}
	abs, err := paths.ValidateWithinRoot(c.RootAbs, name)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// Find returns the cover ref for a previously-stored providerKey, if
// any file with that hash exists on disk regardless of extension.
// Returns (_, false) when nothing is cached. Callers use this to skip
// re-fetching from the provider when the image is already on disk.
func (c *Cache) Find(providerKey string) (coverRef string, found bool) {
	if providerKey == "" {
		return "", false
	}
	h := sha256.Sum256([]byte(providerKey))
	hashHex := hex.EncodeToString(h[:])
	for _, ext := range []string{".jpg", ".png"} {
		name := hashHex + ext
		abs, err := paths.ValidateWithinRoot(c.RootAbs, name)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		return CoverRefPrefix + name, true
	}
	return "", false
}

// AbsPath resolves a cache filename (the leaf, not the full /covers/
// URL) to its validated absolute path. Used by the HTTP handler that
// serves covers. Returns ErrInvalidFilename for anything not matching
// the sha256+ext shape we produce.
func (c *Cache) AbsPath(name string) (string, error) {
	if !validCacheFilename(name) {
		return "", ErrInvalidFilename
	}
	abs, err := paths.ValidateWithinRoot(c.RootAbs, name)
	if err != nil {
		return "", ErrInvalidFilename
	}
	return abs, nil
}

// ContentTypeFor returns the HTTP Content-Type that matches the given
// cache-filename extension. Unknown extensions panic — callers should
// have already filtered through validCacheFilename.
func ContentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".jpg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	default:
		return "application/octet-stream"
	}
}

// cacheFilenamePattern matches sha256 hex + .jpg/.png.
var cacheFilenamePattern = regexp.MustCompile(`^[0-9a-f]{64}\.(jpg|png)$`)

func validCacheFilename(name string) bool {
	return cacheFilenamePattern.MatchString(name)
}

// filenameFromRef extracts the leaf filename from a "/covers/<leaf>"
// ref. Returns (_, false) for any ref that doesn't match our own
// naming convention — which correctly rejects legacy or externally
// supplied cover strings.
func filenameFromRef(coverRef string) (string, bool) {
	if !strings.HasPrefix(coverRef, CoverRefPrefix) {
		return "", false
	}
	name := coverRef[len(CoverRefPrefix):]
	if !validCacheFilename(name) {
		return "", false
	}
	return name, true
}
