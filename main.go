package main

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZdsAlpha/web/internal/content"
	"github.com/ZdsAlpha/web/internal/web"
)

//go:embed all:content
var contentFS embed.FS

//go:embed all:static
var staticFS embed.FS

func main() {
	// In dev mode, read content/static from disk so edits show on rebuild
	// without re-embedding. In prod, use the embedded copies.
	dev := os.Getenv("DEV") == "1"

	var contentRoot, staticRoot fs.FS
	if dev {
		contentRoot = os.DirFS("content")
		staticRoot = os.DirFS("static")
	} else {
		contentRoot = mustSub(contentFS, "content")
		staticRoot = mustSub(staticFS, "static")
	}

	store, err := content.Load(contentRoot, dev) // include drafts only in dev
	if err != nil {
		log.Fatalf("loading content: %v", err)
	}
	log.Printf("loaded %d posts, %d tags", len(store.Posts()), len(store.Tags()))

	addr := ":" + cmp.Or(os.Getenv("PORT"), "8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           web.Handler(store, staticRoot),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s (dev=%v)", addr, dev)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		log.Fatalf("sub fs %q: %v", dir, err)
	}
	return sub
}
