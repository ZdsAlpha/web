package web

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/ZdsAlpha/web/internal/content"
)

// robots serves a permissive robots.txt that points crawlers at the sitemap.
func robots(baseURL string) http.HandlerFunc {
	body := "User-agent: *\nAllow: /\n"
	if baseURL != "" {
		body += fmt.Sprintf("\nSitemap: %s/sitemap.xml\n", baseURL)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type urlSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// sitemap serves an XML sitemap listing the home page, every post (with its
// latest meaningful content date as lastmod) and every standalone page. Drafts
// are absent because the production store is loaded without them.
func sitemap(store *content.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
		set.URLs = append(set.URLs, sitemapURL{Loc: baseURL + "/"})

		for _, p := range store.Posts() {
			u := sitemapURL{Loc: baseURL + "/posts/" + p.Slug}
			lastMod := p.Updated
			if lastMod.IsZero() {
				lastMod = p.Date
			}
			if !lastMod.IsZero() {
				u.LastMod = lastMod.UTC().Format("2006-01-02")
			}
			set.URLs = append(set.URLs, u)
		}
		for _, slug := range store.PageSlugs() {
			set.URLs = append(set.URLs, sitemapURL{Loc: baseURL + "/" + slug})
		}

		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(xml.Header))
		enc := xml.NewEncoder(w)
		enc.Indent("", "  ")
		_ = enc.Encode(set)
		_, _ = w.Write([]byte("\n"))
	}
}
