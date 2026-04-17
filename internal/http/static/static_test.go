package static

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFSContainsExpectedAssets(t *testing.T) {
	want := []string{"app.css", "app.js", "favicon.svg"}
	for _, name := range want {
		f, err := FS().Open(name)
		if err != nil {
			t.Errorf("FS missing %q: %v", name, err)
			continue
		}
		f.Close()
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
