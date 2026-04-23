package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/config"
	httpserver "github.com/inarun/Shelf/internal/http/server"
	"github.com/inarun/Shelf/internal/index/store"
	sync_ "github.com/inarun/Shelf/internal/index/sync"
)

// buildServer stands up the full HTTP stack against a synthetic vault,
// returning (base URL, books folder, cleanup). Mirrors what
// cmd/shelf/main.go does, minus the signal handler and file logger.
func buildServer(t *testing.T) (string, string, func()) {
	t.Helper()
	root := t.TempDir()
	books := filepath.Join(root, "books")
	backups := filepath.Join(root, "backups")
	for _, d := range []string{books, backups} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(books, "Hyperion by Dan Simmons.md")
	if err := os.WriteFile(path, []byte(hyperion), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	sy := sync_.New(st, books)
	if _, err := sy.FullScan(context.Background()); err != nil {
		_ = st.Close()
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Listener must be 127.0.0.1 so the Host middleware allows it (port
	// is dynamic below; host middleware accepts 127.0.0.1:{configPort} so
	// we override Host on every request).
	srv, err := httpserver.New(httpserver.Dependencies{
		Config:      &config.Config{Server: config.ServerConfig{Bind: "127.0.0.1", Port: 7744}},
		Store:       st,
		Syncer:      sy,
		BooksAbs:    books,
		BackupsRoot: backups,
		Logger:      logger,
	})
	if err != nil {
		_ = st.Close()
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	cleanup := func() {
		ts.Close()
		_ = st.Close()
	}
	return ts.URL, books, cleanup
}

// doReq is a small helper that always sets Host to the value the Host
// middleware allowlists (127.0.0.1:7744), so httptest's random port
// doesn't trip us up.
func doReq(t *testing.T, client *http.Client, method, url string, body io.Reader, mutate func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1:7744"
	if mutate != nil {
		mutate(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

var csrfMetaRE = regexp.MustCompile(`<meta\s+name="csrf-token"\s+content="([^"]+)"`)

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	m := csrfMetaRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("csrf meta not found in body:\n%s", body)
	}
	return m[1]
}

func TestEndToEnd_LibraryThenRatingPatchThenImport(t *testing.T) {
	base, books, cleanup := buildServer(t)
	defer cleanup()

	jar, _ := newCookieJar()
	client := &http.Client{Jar: jar}

	// 1. GET /library → HTML with CSRF meta + session cookie.
	resp := doReq(t, client, http.MethodGet, base+"/library", nil, nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("library status = %d; body=%s", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Errorf("missing CSP on /library")
	}
	if !strings.Contains(string(body), "Hyperion") {
		t.Errorf("library page missing Hyperion")
	}
	csrf := extractCSRF(t, string(body))

	// 2. GET /books/{filename} → HTML detail.
	resp = doReq(t, client, http.MethodGet, base+"/books/"+pathEscape("Hyperion by Dan Simmons.md"), nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "Dan Simmons") {
		t.Errorf("detail page missing author")
	}

	// 3. PATCH rating via JSON API (cookie jar carries session cookie).
	// Post-S15 the rating is a dimensioned map (trial_system + overall).
	resp = doReq(t, client, http.MethodPatch,
		base+"/api/books/"+pathEscape("Hyperion by Dan Simmons.md"),
		strings.NewReader(`{"rating": {"overall": 5}}`),
		func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
		})
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d; body=%s", resp.StatusCode, body)
	}

	// Disk must reflect the new rating.
	disk, err := os.ReadFile(filepath.Join(books, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(disk), "overall: 5") {
		t.Errorf("frontmatter rating not updated; content:\n%s", disk)
	}

	// 4. POST /api/import/plan with a small CSV.
	csvText := `Book Id,Title,Author,ISBN,ISBN13,My Rating,Exclusive Shelf,Date Read,Date Added,My Review,Publisher,Year Published,Number of Pages,Bookshelves
1,"Project Hail Mary","Andy Weir","","=""9780593135204""",4,read,2025/02/14,2025/01/05,"Fun.","Ballantine",2021,476,"sci-fi"
`
	mpBody := &bytes.Buffer{}
	w := multipart.NewWriter(mpBody)
	fw, _ := w.CreateFormFile("csv", "export.csv")
	_, _ = fw.Write([]byte(csvText))
	_ = w.Close()
	resp = doReq(t, client, http.MethodPost, base+"/api/import/plan", mpBody, func(r *http.Request) {
		r.Header.Set("Content-Type", w.FormDataContentType())
		r.Header.Set("X-CSRF-Token", csrf)
	})
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan status = %d; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"will_create"`) {
		t.Errorf("plan JSON missing will_create; body:\n%s", body)
	}

	// 5. POST /api/import/apply — rebuild body (multipart is single-use).
	mpBody2 := &bytes.Buffer{}
	w2 := multipart.NewWriter(mpBody2)
	fw2, _ := w2.CreateFormFile("csv", "export.csv")
	_, _ = fw2.Write([]byte(csvText))
	_ = w2.WriteField("decisions", "[]")
	_ = w2.Close()

	resp = doReq(t, client, http.MethodPost, base+"/api/import/apply", mpBody2, func(r *http.Request) {
		r.Header.Set("Content-Type", w2.FormDataContentType())
		r.Header.Set("X-CSRF-Token", csrf)
	})
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply status = %d; body=%s", resp.StatusCode, body)
	}
	var report struct {
		BackupRoot string   `json:"backup_root"`
		Created    []string `json:"created"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("apply report: %v body=%s", err, body)
	}
	if report.BackupRoot == "" {
		t.Error("apply report missing backup_root")
	}
	phm := filepath.Join(books, "Project Hail Mary by Andy Weir.md")
	if _, err := os.Stat(phm); err != nil {
		t.Errorf("Project Hail Mary not created on disk: %v", err)
	}
}

func TestEndToEnd_EvilHostRejected(t *testing.T) {
	base, _, cleanup := buildServer(t)
	defer cleanup()

	req, _ := http.NewRequest(http.MethodGet, base+"/library", nil)
	req.Host = "evil.example.com:7744"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("evil host: status = %d, want 421", resp.StatusCode)
	}
}

func TestEndToEnd_PWAAndHealthz(t *testing.T) {
	base, _, cleanup := buildServer(t)
	defer cleanup()
	client := &http.Client{}

	// /manifest.webmanifest: expected content-type, starts with { and mentions start_url.
	resp := doReq(t, client, http.MethodGet, base+"/manifest.webmanifest", nil, nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest status = %d; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/manifest+json") {
		t.Errorf("manifest Content-Type = %q, want application/manifest+json...", got)
	}
	if !strings.Contains(string(body), "\"start_url\"") {
		t.Errorf("manifest missing start_url: %s", body)
	}
	// CSP still applied to manifest responses.
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Errorf("manifest missing CSP")
	}

	// /sw.js: root-scope service worker.
	resp = doReq(t, client, http.MethodGet, base+"/sw.js", nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sw.js status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
		t.Errorf("sw.js Content-Type = %q", got)
	}
	if got := resp.Header.Get("Service-Worker-Allowed"); got != "/" {
		t.Errorf("sw.js Service-Worker-Allowed = %q, want /", got)
	}
	if !strings.Contains(string(body), "addEventListener(\"fetch\"") {
		t.Errorf("sw.js body missing fetch listener")
	}

	// /healthz: signature body so the single-instance probe can detect us.
	resp = doReq(t, client, http.MethodGet, base+"/healthz", nil, nil)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "shelf") {
		t.Errorf("healthz body = %q, want containing 'shelf'", body)
	}
}

func TestEndToEnd_PatchWithoutCSRF403(t *testing.T) {
	base, _, cleanup := buildServer(t)
	defer cleanup()
	jar, _ := newCookieJar()
	client := &http.Client{Jar: jar}

	// Mint the cookie.
	resp := doReq(t, client, http.MethodGet, base+"/library", nil, nil)
	_ = resp.Body.Close()

	// PATCH without CSRF header — must be rejected even with cookie.
	resp = doReq(t, client, http.MethodPatch,
		base+"/api/books/"+pathEscape("Hyperion by Dan Simmons.md"),
		strings.NewReader(`{"rating": {"overall": 3}}`),
		func(r *http.Request) { r.Header.Set("Content-Type", "application/json") })
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("no CSRF: status = %d, want 403", resp.StatusCode)
	}
}

// pathEscape is url.PathEscape but wrapped so the import surface of the
// test stays small.
func pathEscape(s string) string {
	// Minimal escaping — enough for spaces + the characters in our fixture.
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == ' ':
			out = append(out, '%', '2', '0')
		default:
			out = append(out, byte(r))
		}
	}
	return string(out)
}
