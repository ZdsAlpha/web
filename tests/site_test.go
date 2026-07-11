package tests

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZdsAlpha/web/internal/content"
	"github.com/ZdsAlpha/web/internal/web"
	"github.com/ZdsAlpha/web/view"
)

func TestPublishedContentRoutesAndAssets(t *testing.T) {
	site := newTestSite(t)

	htmlRoutes := []string{"/"}
	for _, post := range site.store.Posts() {
		if post.Draft {
			t.Fatalf("draft post %q appeared in published content", post.Slug)
		}
		if post.Title == "" || post.Description == "" || post.Summary == "" {
			t.Fatalf("post %q should have title, description, and summary", post.Slug)
		}
		htmlRoutes = append(htmlRoutes, "/posts/"+post.Slug)
	}
	for _, slug := range site.store.PageSlugs() {
		htmlRoutes = append(htmlRoutes, "/"+slug)
	}

	for _, route := range htmlRoutes {
		t.Run(route, func(t *testing.T) {
			rec := request(t, site.handler, route, http.StatusOK)
			if rec.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("html response should include security headers")
			}

			body := rec.Body.String()
			requireContains(t, body, `<meta name="viewport" content="width=device-width, initial-scale=1">`)
			requireContains(t, body, `<link rel="icon" href="/static/favicon.svg" type="image/svg+xml">`)
			if strings.Contains(body, "fonts.googleapis.com") || strings.Contains(body, "fonts.gstatic.com") {
				t.Fatal("page should not disclose visitor requests to third-party font hosts")
			}
			if route != "/" {
				requireContains(t, body, `<link rel="canonical" href="https://arehman.dev`)
			}
			for _, assetPath := range staticAssetRefs(body) {
				request(t, site.handler, assetPath, http.StatusOK)
			}
		})
	}
}

func TestPostSEOAndImageLoadingMetadata(t *testing.T) {
	site := newTestSite(t)
	for _, post := range site.store.Posts() {
		rec := request(t, site.handler, "/posts/"+post.Slug, http.StatusOK)
		body := rec.Body.String()
		if post.Image != "" {
			requireContains(t, body, `"image":"https://arehman.dev`+post.Image+`"`)
		}
		if !post.Updated.IsZero() {
			requireContains(t, body, `"dateModified":"`+post.Updated.UTC().Format(time.RFC3339)+`"`)
			requireContains(t, body, `<meta property="article:modified_time" content="`+post.Updated.UTC().Format(time.RFC3339)+`">`)
		}
		if strings.Contains(body, `<img `) {
			requireContains(t, body, `loading="lazy"`)
			requireContains(t, body, `decoding="async"`)
		}
	}
}

func TestWWWRedirectAndStaticCaching(t *testing.T) {
	site := newTestSite(t)
	req := httptest.NewRequest(http.MethodGet, "https://www.arehman.dev/path?q=1", nil)
	rec := httptest.NewRecorder()
	site.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "https://arehman.dev/path?q=1" {
		t.Fatalf("www redirect status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	asset := request(t, site.handler, "/static/favicon.svg", http.StatusOK)
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("favicon Cache-Control=%q", got)
	}
}

func TestResponsiveLayoutContracts(t *testing.T) {
	css := readFile(t, filepath.Join("..", "static", "css", "style.css"))

	if strings.Contains(css, "letter-spacing: -") {
		t.Fatal("negative letter spacing can make narrow layouts harder to read")
	}
	requireContains(t, css, "--pico-font-size: 100%")

	htmlBlock := cssBlock(t, css, `html`)
	requireContains(t, htmlBlock, "font-size: 16px")

	contentBlock := cssBlock(t, css, `\.content`)
	requireContains(t, contentBlock, "width: 100%")
	requireContains(t, contentBlock, "max-width: calc(var(--reading)")
	requireContains(t, contentBlock, "min-width: 0")

	postBlock := cssBlock(t, css, `article\.post`)
	requireContains(t, postBlock, "padding: 0")
	requireContains(t, postBlock, "background: transparent")

	postHeaderBlock := cssBlock(t, css, `article\.post > header\.post-header`)
	requireContains(t, postHeaderBlock, "padding: 0 0")
	requireContains(t, postHeaderBlock, "background: transparent")

	imageBlock := cssBlock(t, css, `\.prose img`)
	requireContains(t, imageBlock, "max-width: 100%")
	requireContains(t, imageBlock, "height: auto")

	tableBlock := cssBlock(t, css, `\.prose table`)
	requireContains(t, tableBlock, "overflow-x: auto")

	mobileBlock := cssMediaBlock(t, css, `@media \(max-width: 640px\)`)
	requireContains(t, mobileBlock, "overflow-x: clip")
	requireContains(t, mobileBlock, "overflow-wrap:")
}

func TestSVGFiguresAreScalableForArticles(t *testing.T) {
	names, err := filepath.Glob(filepath.Join("..", "static", "img", "*.svg"))
	if err != nil {
		t.Fatalf("glob svg figures: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected at least one svg figure")
	}

	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			raw := readFile(t, name)
			var root struct {
				XMLName xml.Name `xml:"svg"`
				Width   string   `xml:"width,attr"`
				Height  string   `xml:"height,attr"`
				ViewBox string   `xml:"viewBox,attr"`
			}
			if err := xml.Unmarshal([]byte(raw), &root); err != nil {
				t.Fatalf("parse svg: %v", err)
			}

			width := positiveIntAttr(t, "width", root.Width)
			positiveIntAttr(t, "height", root.Height)
			if root.ViewBox == "" {
				t.Fatal("svg should include a viewBox so it scales predictably")
			}
			if width > 900 {
				t.Fatalf("svg width %dpx is too wide for an in-article figure", width)
			}

			fontSizes := regexp.MustCompile(`font:\s*\d+\s+(\d+)px`).FindAllStringSubmatch(raw, -1)
			if len(fontSizes) == 0 {
				t.Fatal("svg should use explicit text sizes for predictable rendering")
			}
			for _, match := range fontSizes {
				size, err := strconv.Atoi(match[1])
				if err != nil {
					t.Fatalf("parse font size %q: %v", match[1], err)
				}
				if size < 11 {
					t.Fatalf("svg font size %dpx is too small for article screenshots", size)
				}
			}
		})
	}
}

type testSite struct {
	store   *content.Store
	handler http.Handler
}

func newTestSite(t *testing.T) testSite {
	t.Helper()

	store, err := content.Load(os.DirFS(filepath.Join("..", "content")), false)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	if len(store.Posts()) == 0 {
		t.Fatal("expected at least one published post")
	}

	view.SetBaseURL("https://arehman.dev")
	return testSite{
		store:   store,
		handler: web.Handler(store, os.DirFS(filepath.Join("..", "static")), "https://arehman.dev"),
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func request(t *testing.T, handler http.Handler, path string, want int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("GET %s status=%d, want %d; body=%s", path, rec.Code, want, rec.Body.String())
	}
	return rec
}

func cssBlock(t *testing.T, css, selectorPattern string) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)` + selectorPattern + `\s*\{([^{}]*)\}`)
	match := re.FindStringSubmatch(css)
	if len(match) != 2 {
		t.Fatalf("missing css block for %s", selectorPattern)
	}
	return match[1]
}

func cssMediaBlock(t *testing.T, css, mediaPattern string) string {
	t.Helper()
	startRe := regexp.MustCompile(mediaPattern + `\s*\{`)
	loc := startRe.FindStringIndex(css)
	if loc == nil {
		t.Fatalf("missing media block %s", mediaPattern)
	}
	depth := 0
	for i := loc[1] - 1; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[loc[1]:i]
			}
		}
	}
	t.Fatalf("unterminated media block %s", mediaPattern)
	return ""
}

func staticAssetRefs(html string) []string {
	matches := regexp.MustCompile(`(?:href|src)="(/static/[^"#?]+)`).FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		paths = append(paths, match[1])
	}
	return paths
}

func positiveIntAttr(t *testing.T, name, raw string) int {
	t.Helper()
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("svg %s should be a positive integer, got %q", name, raw)
	}
	return n
}

func requireContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected to find %q in:\n%s", want, got)
	}
}
