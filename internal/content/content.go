// Package content loads markdown documents (blog posts and standalone pages)
// from a filesystem, renders them to HTML once, and serves them from an
// in-memory index. It is deliberately not blog-locked: Posts carry blog
// semantics (date, tags, feed eligibility) while Pages are generic documents.
package content

import (
	"html/template"
	"time"
)

// Document is the shared, rendered form of any markdown file.
type Document struct {
	Slug    string
	Title   string
	HTML    template.HTML // pre-rendered body
	RawText string        // plain text, for reading-time and future search
}

// Post is a dated, tagged blog entry.
type Post struct {
	Document
	Date        time.Time
	Description string
	Tags        []string
	Draft       bool
	Summary     string
	ReadingMins int
}

// Page is a standalone document (about, CV, etc.).
type Page struct {
	Document
	Description string
}

// meta is the frontmatter schema shared by posts and pages.
type meta struct {
	Title       string   `yaml:"title"`
	Date        string   `yaml:"date"`
	Slug        string   `yaml:"slug"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Draft       bool     `yaml:"draft"`
	Summary     string   `yaml:"summary"`
}
