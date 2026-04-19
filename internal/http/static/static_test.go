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

// TestAppCSSSession10DesignSystem guards the typography + motion system
// added in Session 10. If any of these names drop out of app.css the
// corresponding visual refinement will silently regress — so we anchor
// them with cheap string contains checks rather than parsing CSS.
func TestAppCSSSession10DesignSystem(t *testing.T) {
	data, err := fs.ReadFile(FS(), "app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)

	// Typography system — OpenType feature bundles + tabular-nums grouping.
	for _, want := range []string{
		"--font-features-body:",
		"--font-features-display:",
		"font-feature-settings: var(--font-features-body)",
		"font-feature-settings: var(--font-features-display)",
		"font-variant-numeric: tabular-nums",
		// Per-size letter-spacing overrides from the Session 10 pass.
		"letter-spacing: -0.022em",
		"letter-spacing: -0.015em",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %q — Session 10 typography system", want)
		}
	}

	// Motion system — main fade-in + book-grid stagger + button active scale.
	for _, want := range []string{
		"--stagger-step:",
		"@keyframes shelf-main-in",
		"@keyframes shelf-card-in",
		"animation: shelf-main-in",
		"animation: shelf-card-in",
		".book-grid > .book-card:nth-child(1)",
		".book-grid > .book-card:nth-child(12)",
		"calc(var(--stagger-step) * 1)",
		"translateY(1px) scale(0.98)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %q — Session 10 motion system", want)
		}
	}

	// Brand/logo styles — the Session 10 logo + wordmark pair.
	for _, want := range []string{
		"header.site .brand-mark",
		"header.site .brand-wordmark",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %q — Session 10 brand styles", want)
		}
	}

	// Reduced-motion opt-out must still cover animations too — Session 10
	// added @keyframes so the global kill-switch matters more than before.
	if !strings.Contains(css, "prefers-reduced-motion") || !strings.Contains(css, "animation: none !important") {
		t.Errorf("app.css must retain the reduced-motion animation kill-switch")
	}
}
