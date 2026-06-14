package content

import (
	"fmt"
	"io/fs"
	"html/template"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
)

// Store is the in-memory index of all loaded content.
type Store struct {
	posts      []*Post
	postBySlug map[string]*Post
	pageBySlug map[string]*Page
	tagToPosts map[string][]*Post
}

// Load walks fsys for posts under "posts/" and pages under "pages/", renders
// them, and builds the index. When includeDrafts is false, posts with
// draft: true are skipped.
func Load(fsys fs.FS, includeDrafts bool) (*Store, error) {
	r := newRenderer()
	s := &Store{
		postBySlug: map[string]*Post{},
		pageBySlug: map[string]*Page{},
		tagToPosts: map[string][]*Post{},
	}

	if err := s.loadPosts(fsys, r, includeDrafts); err != nil {
		return nil, err
	}
	if err := s.loadPages(fsys, r); err != nil {
		return nil, err
	}

	// Newest first; tie-break on slug for stable ordering.
	sort.Slice(s.posts, func(i, j int) bool {
		if s.posts[i].Date.Equal(s.posts[j].Date) {
			return s.posts[i].Slug < s.posts[j].Slug
		}
		return s.posts[i].Date.After(s.posts[j].Date)
	})

	return s, nil
}

func (s *Store) loadPosts(fsys fs.FS, r *renderer, includeDrafts bool) error {
	return fs.WalkDir(fsys, "posts", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}

		var m meta
		body, err := readDoc(fsys, p, &m)
		if err != nil {
			return fmt.Errorf("posts/%s: %w", p, err)
		}
		if m.Draft && !includeDrafts {
			return nil
		}

		date, err := parseDate(m.Date)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}

		html, err := r.render(body)
		if err != nil {
			return fmt.Errorf("%s: render: %w", p, err)
		}

		slug := slugFor(m.Slug, p)
		if _, dup := s.postBySlug[slug]; dup {
			return fmt.Errorf("%s: duplicate slug %q", p, slug)
		}

		raw := string(body)
		post := &Post{
			Document: Document{
				Slug:    slug,
				Title:   m.Title,
				HTML:    template.HTML(html),
				RawText: raw,
			},
			Date:        date,
			Description: m.Description,
			Tags:        m.Tags,
			Draft:       m.Draft,
			Summary:     summaryFor(m.Summary, m.Description, raw),
			ReadingMins: readingMinutes(raw),
		}
		s.posts = append(s.posts, post)
		s.postBySlug[slug] = post
		for _, t := range m.Tags {
			s.tagToPosts[t] = append(s.tagToPosts[t], post)
		}
		return nil
	})
}

func (s *Store) loadPages(fsys fs.FS, r *renderer) error {
	return fs.WalkDir(fsys, "pages", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}

		var m meta
		body, err := readDoc(fsys, p, &m)
		if err != nil {
			return fmt.Errorf("pages/%s: %w", p, err)
		}
		html, err := r.render(body)
		if err != nil {
			return fmt.Errorf("%s: render: %w", p, err)
		}
		slug := slugFor(m.Slug, p)
		if _, dup := s.pageBySlug[slug]; dup {
			return fmt.Errorf("%s: duplicate slug %q", p, slug)
		}
		s.pageBySlug[slug] = &Page{
			Document: Document{
				Slug:    slug,
				Title:   m.Title,
				HTML:    template.HTML(html),
				RawText: string(body),
			},
			Description: m.Description,
		}
		return nil
	})
}

// Accessors.

func (s *Store) Posts() []*Post { return s.posts }

func (s *Store) Post(slug string) (*Post, bool) {
	p, ok := s.postBySlug[slug]
	return p, ok
}

func (s *Store) Page(slug string) (*Page, bool) {
	p, ok := s.pageBySlug[slug]
	return p, ok
}

func (s *Store) PostsByTag(tag string) []*Post { return s.tagToPosts[tag] }

// PageSlugs returns the slugs of all standalone pages, sorted for stable output.
func (s *Store) PageSlugs() []string {
	slugs := make([]string, 0, len(s.pageBySlug))
	for slug := range s.pageBySlug {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

func (s *Store) Tags() []string {
	tags := make([]string, 0, len(s.tagToPosts))
	for t := range s.tagToPosts {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// Helpers.

func readDoc(fsys fs.FS, p string, m *meta) ([]byte, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return frontmatter.Parse(f, m)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slugFor uses the frontmatter slug if set, else derives one from the
// filename, stripping a leading YYYY-MM-DD- date prefix if present.
func slugFor(fmSlug, filePath string) string {
	if fmSlug != "" {
		return fmSlug
	}
	name := strings.TrimSuffix(path.Base(filePath), ".md")
	name = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`).ReplaceAllString(name, "")
	name = strings.ToLower(name)
	name = nonSlug.ReplaceAllString(name, "-")
	return strings.Trim(name, "-")
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("missing date")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable date %q", s)
}

func readingMinutes(raw string) int {
	words := len(strings.Fields(raw))
	mins := words / 200
	if mins < 1 {
		return 1
	}
	return mins
}

func summaryFor(summary, description, raw string) string {
	if summary != "" {
		return summary
	}
	if description != "" {
		return description
	}
	// Fall back to the first non-empty line of the body.
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}
