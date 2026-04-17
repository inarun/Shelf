package static

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFSContainsExpectedAssets(t *testing.T) {
	want := []string{"app.css", "app.js", "favicon.svg", "manifest.webmanifest", "sw.js"}
	for _, name := range want {
		f, err := FS().Open(name)
		if err != nil {
			t.Errorf("FS missing %q: %v", name, err)
			continue
		}
		f.Close()
	}
}

func TestManifestHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	w := httptest.NewRecorder()
	ManifestHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/manifest+json") {
		t.Errorf("manifest Content-Type = %q, want application/manifest+json...", ct)
	}
	if !strings.Contains(w.Body.String(), "\"start_url\"") {
		t.Error("manifest body missing start_url")
	}
}

func TestServiceWorkerHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	w := httptest.NewRecorder()
	ServiceWorkerHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sw status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("sw Content-Type = %q, want text/javascript...", ct)
	}
	if got := w.Header().Get("Service-Worker-Allowed"); got != "/" {
		t.Errorf("Service-Worker-Allowed = %q, want /", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, "addEventListener(\"fetch\"") {
		t.Error("sw body missing fetch handler")
	}
}

func TestServiceWorkerNoExternalURLs(t *testing.T) {
	data, err := fs.ReadFile(FS(), "sw.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://", "https://", "cdn.", "googleapis", "jsdelivr"} {
		if strings.Contains(string(data), bad) {
			t.Errorf("sw.js contains forbidden external reference %q", bad)
		}
	}
}

func TestHandlerServesAssets(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", Handler()))

	for _, tc := range []struct {
		path        string
		wantContent string
	}{
		{"/static/app.css", "book-card"},
		{"/static/app.js", "csrf-token"},
		{"/static/favicon.svg", "<svg"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.path, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), tc.wantContent) {
			t.Errorf("%s: body missing %q", tc.path, tc.wantContent)
		}
		if got := w.Header().Get("Cache-Control"); got == "" {
			t.Errorf("%s: expected Cache-Control header", tc.path)
		}
	}
}

func TestHandler404ForUnknownAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", Handler()))
	req := httptest.NewRequest(http.MethodGet, "/static/nope.css", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown asset: status = %d, want 404", w.Code)
	}
}

func TestAppJSNoExternalURLs(t *testing.T) {
	data, err := fs.ReadFile(FS(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://", "https://", "cdn.", "googleapis", "jsdelivr"} {
		if strings.Contains(string(data), bad) {
			t.Errorf("app.js contains forbidden external reference %q — SKILL.md forbids external resources", bad)
		}
	}
}
