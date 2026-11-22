// Package web wires HTTP routes to the content store and templ views.
package web

import (
	"bytes"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ZdsAlpha/web/internal/content"
	"github.com/ZdsAlpha/web/view"
	"github.com/a-h/templ"
)

// Handler builds the application's http.Handler from the content store and the
// embedded static asset filesystem (rooted so that "css/..." resolves). baseURL
// is the site's absolute origin (e.g. "https://arehman.dev"), used to emit
// robots.txt and an absolute-URL sitemap.
func Handler(store *content.Store, staticFS fs.FS, baseURL string) http.Handler {
	mux := http.NewServeMux()

	// noDirFS disables directory listings; only files are served.
	staticHandler := http.StripPrefix("/static/", http.FileServerFS(noDirFS{staticFS}))
	mux.Handle("GET /static/", cacheStatic(staticHandler))
	mux.HandleFunc("GET "+imgen40ModelPath, func(w http.ResponseWriter, r *http.Request) {
		if requested := r.URL.Query().Get("v"); requested != "" {
			version, _ := imgen40Model(staticFS)
			if requested != version {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// Give the model transfer time on slower connections.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
		staticHandler.ServeHTTP(w, r)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /robots.txt", robots(baseURL))
	mux.HandleFunc("GET /sitemap.xml", sitemap(store, baseURL))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, http.StatusOK, view.Home(store.Posts()))
	})

	mux.HandleFunc("GET /posts/{slug}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := store.Post(r.PathValue("slug"))
		if !ok {
			notFound(w, r)
			return
		}
		render(w, r, http.StatusOK, view.Post(p))
	})
	mux.HandleFunc("GET /demos", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, http.StatusOK, view.Demos())
	})
	mux.HandleFunc("GET /demos/imgen40", func(w http.ResponseWriter, r *http.Request) {
		target := url.URL{Path: view.StableGANPath, RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET "+view.StableGANPath, func(w http.ResponseWriter, r *http.Request) {
		version, modelBytes := imgen40Model(staticFS)
		var modelURL string
		if version != "" {
			modelURL = imgen40ModelPath + "?v=" + version
		}
		render(w, r, http.StatusOK, view.StableGANPrototype(modelURL, modelBytes))
	})

	// Standalone pages live at the root (e.g. /about). Registered as a
	// catch-all so it runs last; unknown slugs fall through to 404.
	mux.HandleFunc("GET /{slug}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := store.Page(r.PathValue("slug"))
		if !ok {
			notFound(w, r)
			return
		}
		render(w, r, http.StatusOK, view.Page(p))
	})

	return securityHeaders(canonicalHostRedirect(mux, baseURL))
}

// canonicalHostRedirect permanently redirects the conventional www alias to
// the configured canonical origin. Other hosts (including local development)
// pass through unchanged.
func canonicalHostRedirect(next http.Handler, baseURL string) http.Handler {
	origin, err := url.Parse(baseURL)
	if err != nil || origin.Scheme == "" || origin.Hostname() == "" {
		return next
	}
	wwwHost := "www." + origin.Hostname()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHost := r.Host
		if host, _, err := net.SplitHostPort(requestHost); err == nil {
			requestHost = host
		}
		if !strings.EqualFold(requestHost, wwwHost) {
			next.ServeHTTP(w, r)
			return
		}

		target := *r.URL
		target.Scheme = origin.Scheme
		target.Host = origin.Host
		http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
	})
}

// cacheStatic allows short-lived browser and edge caching without requiring
// fingerprinted filenames. An hour keeps deploys responsive while avoiding a
// full asset transfer on every page view.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cacheControl := "public, max-age=3600"
		switch r.URL.Path {
		case "/static/js/imgen40.js", imgen40WorkerPath:
			// The page and worker share a message format and should update together.
			cacheControl = "no-cache"
		}
		w.Header().Set("Cache-Control", cacheControl)
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets conservative security headers on every response. The CSP
// allows only same-origin resources; there are no inline scripts or styles (the
// theme bootstrap lives in /static/js/theme-init.js). Only the generator worker
// needs WebAssembly compilation.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"style-src 'self'; " +
		"font-src 'self'; " +
		"img-src 'self' data:; " +
		"base-uri 'none'; " +
		"frame-ancestors 'none'; " +
		"form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		scriptSrc := "script-src 'self'"
		if r.URL.Path == imgen40WorkerPath {
			scriptSrc += " 'wasm-unsafe-eval'"
		}
		h.Set("Content-Security-Policy", csp+"; "+scriptSrc)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// noDirFS wraps an fs.FS and returns fs.ErrNotExist for directories, which makes
// http.FileServer respond 404 to directory paths instead of listing contents.
type noDirFS struct{ fs.FS }

func (f noDirFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}

// render writes the component to a buffer first so a render failure yields a
// clean 500 instead of a truncated 200.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, view.NotFound())
}
