package content

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestLoadRejectsInvalidExplicitSlug(t *testing.T) {
	fsys := fstest.MapFS{
		"posts/post.md":  {Data: []byte("---\ntitle: Post\ndate: 2026-01-01\nslug: bad/slug\n---\nBody")},
		"pages/about.md": {Data: []byte("---\ntitle: About\n---\nBody")},
	}
	if _, err := Load(fsys, false); err == nil {
		t.Fatal("Load() accepted a slug that cannot be routed")
	}
}

func TestPostsByTagAreNewestFirst(t *testing.T) {
	fsys := fstest.MapFS{
		"posts/a.md":     {Data: []byte("---\ntitle: Older\ndate: 2025-01-01\ntags: [go]\n---\nBody")},
		"posts/z.md":     {Data: []byte("---\ntitle: Newer\ndate: 2026-01-01\ntags: [go]\n---\nBody")},
		"pages/about.md": {Data: []byte("---\ntitle: About\n---\nBody")},
	}
	store, err := Load(fsys, false)
	if err != nil {
		t.Fatal(err)
	}
	posts := store.PostsByTag("go")
	if len(posts) != 2 || posts[0].Title != "Newer" {
		t.Fatalf("PostsByTag(go) = %#v; want newest post first", posts)
	}
}

func TestLoadPostSEOFields(t *testing.T) {
	fsys := fstest.MapFS{
		"posts/post.md":  {Data: []byte("---\ntitle: Post\ndate: 2026-01-01\nupdated: 2026-02-03\nimage: /static/img/post.svg\n---\n![Figure](/static/img/post.svg)")},
		"pages/about.md": {Data: []byte("---\ntitle: About\n---\nBody")},
	}
	store, err := Load(fsys, false)
	if err != nil {
		t.Fatal(err)
	}
	post, ok := store.Post("post")
	if !ok {
		t.Fatal("loaded store omitted post")
	}
	if want := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC); !post.Updated.Equal(want) {
		t.Fatalf("Updated = %v; want %v", post.Updated, want)
	}
	if post.Image != "/static/img/post.svg" {
		t.Fatalf("Image = %q; want /static/img/post.svg", post.Image)
	}
	for _, attr := range []string{`loading="lazy"`, `decoding="async"`} {
		if !strings.Contains(string(post.HTML), attr) {
			t.Fatalf("rendered image omitted %s: %s", attr, post.HTML)
		}
	}
}

func TestLoadRejectsUpdatedBeforePublished(t *testing.T) {
	fsys := fstest.MapFS{
		"posts/post.md":  {Data: []byte("---\ntitle: Post\ndate: 2026-02-01\nupdated: 2026-01-01\n---\nBody")},
		"pages/about.md": {Data: []byte("---\ntitle: About\n---\nBody")},
	}
	if _, err := Load(fsys, false); err == nil {
		t.Fatal("Load() accepted an updated date before the published date")
	}
}
