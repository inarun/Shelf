package covers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/providers/metadata"
)

func TestNew_CreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "covers")
	c, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(c.RootAbs); err != nil {
		t.Fatalf("root not created: %v", err)
	}
}

func TestNew_RejectsEmptyRoot(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestStore_RoundtripAndDedup(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	img := &metadata.CoverImage{
		Bytes:       []byte{0xff, 0xd8, 0xff, 0xe0, 'x'},
		ContentType: "image/jpeg",
		Ext:         ".jpg",
	}
	ref1, err := c.Store(ProviderKey("openlibrary", "olid:OL123M"), img)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if !strings.HasPrefix(ref1, "/covers/") {
		t.Errorf("ref prefix: %q", ref1)
	}
	if !strings.HasSuffix(ref1, ".jpg") {
		t.Errorf("ref suffix: %q", ref1)
	}
	if !c.Exists(ref1) {
		t.Error("Exists should be true after Store")
	}

	// Same provider key ⇒ same filename even with different bytes.
	img2 := &metadata.CoverImage{
		Bytes:       []byte{0xff, 0xd8, 0xff, 0xe0, 'y', 'z'},
		ContentType: "image/jpeg",
		Ext:         ".jpg",
	}
	ref2, err := c.Store(ProviderKey("openlibrary", "olid:OL123M"), img2)
	if err != nil {
		t.Fatalf("Store (rewrite): %v", err)
	}
	if ref1 != ref2 {
		t.Errorf("expected dedup to same ref; got %q vs %q", ref1, ref2)
	}
	// Different provider key ⇒ different filename.
	ref3, err := c.Store(ProviderKey("openlibrary", "olid:OL999M"), img)
	if err != nil {
		t.Fatalf("Store (different key): %v", err)
	}
	if ref3 == ref1 {
		t.Error("expected distinct ref for distinct provider key")
	}
}

func TestStore_RejectsUnsupportedExt(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	img := &metadata.CoverImage{
		Bytes: []byte{0x01}, Ext: ".gif", ContentType: "image/gif",
	}
	if _, err := c.Store(ProviderKey("x", "y"), img); err == nil {
		t.Fatal("expected ext rejection")
	}
}

func TestStore_RejectsEmptyBytes(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Store("x/y", &metadata.CoverImage{Ext: ".jpg"}); err == nil {
		t.Fatal("expected empty-bytes rejection")
	}
}

func TestAbsPath_ValidAndInvalid(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	good := strings.Repeat("a", 64) + ".jpg"
	if _, err := c.AbsPath(good); err != nil {
		t.Errorf("AbsPath(%q): %v", good, err)
	}

	bad := []string{
		"",
		"notahash.jpg",
		strings.Repeat("a", 64) + ".gif",
		"../secret.jpg",
		strings.Repeat("a", 63) + ".jpg",
		strings.Repeat("A", 64) + ".jpg", // uppercase hex not allowed
	}
	for _, name := range bad {
		if _, err := c.AbsPath(name); err == nil {
			t.Errorf("AbsPath(%q) should reject", name)
		}
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"aaa.jpg":               "image/jpeg",
		"bbb.png":               "image/png",
		"unknown.bin":           "application/octet-stream",
	}
	for name, want := range cases {
		if got := ContentTypeFor(name); got != want {
			t.Errorf("ContentTypeFor(%q): got %q want %q", name, got, want)
		}
	}
}

func TestFind_Hit(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	img := &metadata.CoverImage{
		Bytes: []byte{0xff, 0xd8, 0xff, 0xe0, 'x'}, Ext: ".jpg", ContentType: "image/jpeg",
	}
	key := ProviderKey("openlibrary", "olid:OL1M")
	ref, err := c.Store(key, img)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	found, ok := c.Find(key)
	if !ok {
		t.Fatal("Find should report hit")
	}
	if found != ref {
		t.Errorf("Find returned %q, want %q", found, ref)
	}
}

func TestFind_Miss(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := c.Find(ProviderKey("openlibrary", "olid:OL-never-seen")); ok {
		t.Error("Find should miss")
	}
	if _, ok := c.Find(""); ok {
		t.Error("Find with empty key should miss")
	}
}

func TestExists_RejectsForeignRefs(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "covers"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []string{
		"",
		"https://example.com/x.jpg",
		"/covers/../etc/passwd",
		"/covers/shortname.jpg",
	}
	for _, ref := range cases {
		if c.Exists(ref) {
			t.Errorf("Exists(%q) should be false", ref)
		}
	}
}
