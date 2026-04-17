package browser

import (
	"errors"
	"testing"
)

func TestValidateLoopbackURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"http loopback", "http://127.0.0.1:7744/library", false},
		{"http localhost", "http://localhost:7744/", false},
		{"http ipv6 loopback", "http://[::1]:7744/", false},
		{"http localhost path with query", "http://localhost:7744/library?a=1", false},
		{"https loopback ok", "https://127.0.0.1:7744/", false},
		{"empty", "", true},
		{"scheme missing", "127.0.0.1:7744", true},
		{"file scheme", "file:///C:/Windows/System32/calc.exe", true},
		{"javascript scheme", "javascript:alert(1)", true},
		{"external host", "http://example.com/", true},
		{"external ip", "http://8.8.8.8/", true},
		{"userinfo present", "http://user:pass@127.0.0.1:7744/", true},
		{"control char", "http://127.0.0.1:7744/\x00", true},
		{"whitespace scheme", " http://127.0.0.1/", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateLoopbackURL(tc.in)
			if tc.wantErr && err == nil {
				t.Errorf("validateLoopbackURL(%q) = nil, want error", tc.in)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateLoopbackURL(%q) = %v, want nil", tc.in, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrUnsafeURL) {
				t.Errorf("validateLoopbackURL(%q) err = %v, want errors.Is(ErrUnsafeURL)", tc.in, err)
			}
		})
	}
}

// TestOpenRejectsUnsafe verifies the exported Open() refuses unsafe
// URLs before attempting any platform side-effect. The platform code
// is not exercised here — its branches are covered by integration /
// manual smoke tests because there's no sandbox-safe way to assert
// "the default browser launched".
func TestOpenRejectsUnsafe(t *testing.T) {
	t.Parallel()
	err := Open("file:///etc/passwd")
	if err == nil {
		t.Fatal("Open(file://...) = nil, want error")
	}
	if !errors.Is(err, ErrUnsafeURL) {
		t.Errorf("Open(file://...) err = %v, want ErrUnsafeURL", err)
	}
}
