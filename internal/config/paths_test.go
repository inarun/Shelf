package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryDir(t *testing.T) {
	dir, err := BinaryDir()
	if err != nil {
		t.Fatalf("BinaryDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("BinaryDir should return an absolute path, got %q", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("BinaryDir %q: stat: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("BinaryDir %q is not a directory", dir)
	}
}
