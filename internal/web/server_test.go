package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ZdsAlpha/web/internal/content"
)

func TestHandlerRoutes(t *testing.T) {
	t.Parallel()

	h := testHandler(t)
	tests := []struct {
		name        string
		path        string
		status      int
		contentType string
		body        string
	}{
		{name: "home", path: "/", status: http.StatusOK, contentType: "text/html", body: "Writing"},
		{name: "post", path: "/posts/hello", status: http.StatusOK, contentType: "text/html", body: "Hello"},
		{name: "page", path: "/about", status: http.StatusOK, contentType: "text/html", body: "About"},
		{name: "health", path: "/healthz", status: http.StatusOK, body: "ok"},
		{name: "robots", path: "/robots.txt", status: http.StatusOK, contentType: "text/plain", body: "Sitemap: https://example.test/sitemap.xml"},
		{name: "sitemap", path: "/sitemap.xml", status: http.StatusOK, contentType: "application/xml", body: "https://example.test/posts/hello"},
		{name: "static file", path: "/static/app.txt", status: http.StatusOK, contentType: "text/plain", body: "asset"},
		{name: "missing page", path: "/missing", status: http.StatusNotFound, contentType: "text/html", body: "404"},
		{name: "missing post", path: "/posts/missing", status: http.StatusNotFound, contentType: "text/html", body: "404"},
		{name: "no directory listing", path: "/static/", status: http.StatusNotFound, contentType: "text/plain", body: "404"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.status {
				t.Fatalf("GET %s status = %d; want %d", tt.path, rec.Code, tt.status)
			}
			if tt.contentType != "" && !strings.HasPrefix(rec.Header().Get("Content-Type"), tt.contentType) {
				t.Errorf("GET %s Content-Type = %q; want prefix %q", tt.path, rec.Header().Get("Content-Type"), tt.contentType)
			}
			if !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("GET %s body = %q; want it to contain %q", tt.path, rec.Body.String(), tt.body)
			}
			if rec.Header().Get("Content-Security-Policy") == "" {
				t.Errorf("GET %s omitted Content-Security-Policy", tt.path)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("GET %s X-Content-Type-Options = %q; want nosniff", tt.path, got)
			}
		})
	}
}

func TestHandlerRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status = %d; want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlerRedirectsWWWToCanonicalHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://www.example.test/posts/hello?ref=test", nil)
	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("www status = %d; want %d", rec.Code, http.StatusPermanentRedirect)
	}
	if got, want := rec.Header().Get("Location"), "https://example.test/posts/hello?ref=test"; got != want {
		t.Fatalf("Location = %q; want %q", got, want)
	}
}

func TestStaticAssetsAreCached(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.txt", nil))
	if got, want := rec.Header().Get("Cache-Control"), "public, max-age=3600"; got != want {
		t.Fatalf("Cache-Control = %q; want %q", got, want)
	}
}

func TestCSPUsesOnlyLocalStylesAndFonts(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "googleapis.com") || strings.Contains(csp, "gstatic.com") {
		t.Fatalf("CSP permits third-party font requests: %q", csp)
	}
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()

	contentFS := fstest.MapFS{
		"posts/hello.md": {Data: []byte("---\ntitle: Hello\ndate: 2026-01-02\ndescription: A post\ntags: [go]\n---\nPost body")},
		"pages/about.md": {Data: []byte("---\ntitle: About\ndescription: About page\n---\nPage body")},
	}
	store, err := content.Load(contentFS, false)
	if err != nil {
		t.Fatalf("load test content: %v", err)
	}
	staticFS := fstest.MapFS{"app.txt": {Data: []byte("asset")}}
	return Handler(store, staticFS, "https://example.test")
}
