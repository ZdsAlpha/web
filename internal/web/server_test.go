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
		{name: "vault is detached", path: "/vault", status: http.StatusNotFound, contentType: "text/html", body: "404"},
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

func TestVaultWorkspaceIsUnlinkedAndNoIndex(t *testing.T) {
	t.Parallel()

	h := testVaultHandler(t)
	vault := httptest.NewRecorder()
	h.ServeHTTP(vault, httptest.NewRequest(http.MethodGet, "/vault", nil))
	body := vault.Body.String()
	if !strings.Contains(body, `<meta name="robots" content="noindex, nofollow, noarchive">`) {
		t.Fatal("vault workspace should be marked noindex")
	}
	if strings.Contains(body, `<link rel="canonical"`) {
		t.Fatal("vault workspace should not emit a canonical URL")
	}
	if !strings.Contains(body, `id="upload-files"`) || !strings.Contains(body, `id="delete-files"`) {
		t.Fatal("vault workspace omitted permission-controlled file actions")
	}
	if !strings.Contains(body, `id="known-vault-bucket"`) || !strings.Contains(body, `id="bucket-options"`) {
		t.Fatal("vault workspace omitted known-bucket onboarding controls")
	}
	if !strings.Contains(body, `id="upload-files" type="button" disabled`) || !strings.Contains(body, `id="delete-files" type="button" disabled`) {
		t.Fatal("upload/delete actions must start disabled until permission is verified")
	}

	sitemap := httptest.NewRecorder()
	h.ServeHTTP(sitemap, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if strings.Contains(sitemap.Body.String(), "/vault") {
		t.Fatal("vault workspace should not appear in the sitemap")
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

func TestVaultAssetsAreRevalidated(t *testing.T) {
	t.Parallel()

	h := http.StripPrefix("/static/", cacheStatic(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("vault"))
	})))
	for _, path := range []string{"/static/js/vault.js", "/static/css/style.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got, want := rec.Header().Get("Cache-Control"), "no-cache"; got != want {
			t.Errorf("%s Cache-Control = %q; want %q", path, got, want)
		}
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
	if strings.Contains(csp, "connect-src") {
		t.Fatalf("public CSP should not grant vault-only external connections: %q", csp)
	}
}

func TestVaultCSPIsolatedFromPublicCSP(t *testing.T) {
	t.Parallel()

	contentFS := fstest.MapFS{
		"posts/hello.md": {Data: []byte("---\ntitle: Hello\ndate: 2026-01-02\n---\nHello")},
		"pages/about.md": {Data: []byte("---\ntitle: About\n---\nAbout")},
	}
	store, err := content.Load(contentFS, false)
	if err != nil {
		t.Fatalf("load test content: %v", err)
	}
	staticFS := fstest.MapFS{"app.txt": {Data: []byte("asset")}}
	h := HandlerWithVault(store, staticFS, "https://example.test", NewVaultProxy(VaultConfig{S3ProxyEnabled: true}))

	vault := httptest.NewRecorder()
	h.ServeHTTP(vault, httptest.NewRequest(http.MethodGet, "/vault", nil))
	if !strings.Contains(vault.Header().Get("Content-Security-Policy"), "connect-src 'self' https:") {
		t.Fatalf("vault CSP should allow isolated storage connections: %q", vault.Header().Get("Content-Security-Policy"))
	}

	public := httptest.NewRecorder()
	h.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/about", nil))
	if strings.Contains(public.Header().Get("Content-Security-Policy"), "connect-src") {
		t.Fatalf("public CSP inherited vault connection permissions: %q", public.Header().Get("Content-Security-Policy"))
	}
}

func testHandler(t *testing.T) http.Handler {
	return buildTestHandler(t, nil)
}

func testVaultHandler(t *testing.T) http.Handler {
	return buildTestHandler(t, NewVaultProxy(VaultConfig{}))
}

func buildTestHandler(t *testing.T, vault http.Handler) http.Handler {
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
	return HandlerWithVault(store, staticFS, "https://example.test", vault)
}
