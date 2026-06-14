// Package web wires HTTP routes to the content store and templ views.
package web

import (
	"io/fs"
	"net/http"

	"github.com/ZdsAlpha/web/internal/content"
	"github.com/ZdsAlpha/web/view"
	"github.com/a-h/templ"
)

// Handler builds the application's http.Handler from the content store and the
// embedded static asset filesystem (rooted so that "css/..." resolves).
func Handler(store *content.Store, staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

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

	return mux
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(r.Context(), w)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, view.NotFound())
}
