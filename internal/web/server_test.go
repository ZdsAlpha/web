package web

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ZdsAlpha/web/internal/content"
	"github.com/ZdsAlpha/web/view"
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
		{name: "demos", path: "/demos", status: http.StatusOK, contentType: "text/html", body: `href="/demos/stable-gan"`},
		{name: "Stable GAN prototype", path: "/demos/stable-gan", status: http.StatusOK, contentType: "text/html", body: "<h1>Stable GAN Prototype</h1>"},
		{name: "generator without model", path: view.StableGANPath, status: http.StatusOK, contentType: "text/html", body: "The model is unavailable"},
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

func TestLegacyDemoRedirectsToStableGAN(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	testHandler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/imgen40?ref=writing", nil))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("legacy demo status = %d, want 308", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/demos/stable-gan?ref=writing" {
		t.Errorf("legacy demo redirected to %q", got)
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

func TestWASMCompilationIsLimitedToGeneratorWorker(t *testing.T) {
	t.Parallel()

	h := testHandler(t)
	for _, path := range []string{"/", view.StableGANPath, imgen40WorkerPath} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		allowed := strings.Contains(rec.Header().Get("Content-Security-Policy"), "'wasm-unsafe-eval'")
		if allowed != (path == imgen40WorkerPath) {
			t.Errorf("unexpected WebAssembly compilation policy for %s", path)
		}
	}
}

func TestVersionedGeneratorModel(t *testing.T) {
	t.Parallel()

	const modelData = "example model"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(modelData)))
	modelURL := imgen40ModelPath + "?v=" + digest[:12]
	assets := fstest.MapFS{
		imgen40ModelFile:            {Data: []byte(modelData)},
		"models/imgen40/SHA256SUMS": {Data: []byte(digest + "  static/" + imgen40ModelFile + "\n")},
	}
	h := Handler(&content.Store{}, assets, "https://example.test")

	demo := httptest.NewRecorder()
	h.ServeHTTP(demo, httptest.NewRequest(http.MethodGet, view.StableGANPath, nil))
	if !strings.Contains(demo.Body.String(), `data-model-url="`+modelURL+`"`) {
		t.Fatal("demo omitted the versioned model URL")
	}

	model := httptest.NewRecorder()
	h.ServeHTTP(model, httptest.NewRequest(http.MethodGet, modelURL, nil))
	if got := model.Body.String(); model.Code != http.StatusOK || got != modelData {
		t.Fatalf("versioned model returned %d %q", model.Code, got)
	}
	if got := model.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("versioned model Cache-Control = %q", got)
	}

	stale := httptest.NewRecorder()
	h.ServeHTTP(stale, httptest.NewRequest(http.MethodGet, imgen40ModelPath+"?v=old", nil))
	if stale.Code != http.StatusNotFound {
		t.Errorf("stale model URL returned %d, want 404", stale.Code)
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
