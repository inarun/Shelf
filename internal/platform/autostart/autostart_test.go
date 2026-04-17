package autostart

import "testing"

func TestNewValidates(t *testing.T) {
	t.Parallel()

	if _, err := New("", "x"); err == nil {
		t.Error("New(empty name) = nil error, want error")
	}
	if _, err := New("x", ""); err == nil {
		t.Error("New(empty command) = nil error, want error")
	}
	a, err := New("Shelf", `"C:\Shelf\shelf.exe"`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.name != "Shelf" || a.command != `"C:\Shelf\shelf.exe"` {
		t.Errorf("New: bad fields: %+v", a)
	}
}
