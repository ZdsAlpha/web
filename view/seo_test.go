package view

import (
	"strings"
	"testing"
	"time"

	"github.com/ZdsAlpha/web/internal/content"
)

func TestLDScriptEscapesScriptBreakout(t *testing.T) {
	t.Parallel()

	got := string(ldScript(map[string]string{"name": `</script><script>alert("x")</script>`}))
	if strings.Contains(got, `</script><script>`) {
		t.Fatalf("ldScript emitted an executable script breakout: %s", got)
	}
	if !strings.Contains(got, `\u003c/script\u003e`) {
		t.Fatalf("ldScript did not JSON-escape angle brackets: %s", got)
	}
	if strings.Count(got, "</script>") != 1 {
		t.Fatalf("ldScript emitted an unexpected script closing tag: %s", got)
	}
}

func TestBlogPostingLDIncludesImageAndModifiedDate(t *testing.T) {
	previous := baseURL
	t.Cleanup(func() { baseURL = previous })
	SetBaseURL("https://example.test")

	post := &content.Post{
		Document: content.Document{Slug: "post", Title: "Post"},
		Date:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Updated:  time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
		Image:    "/static/img/post.svg",
	}
	got := string(blogPostingLD(post))
	for _, want := range []string{
		`"datePublished":"2026-01-02T00:00:00Z"`,
		`"dateModified":"2026-02-03T00:00:00Z"`,
		`"image":"https://example.test/static/img/post.svg"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("BlogPosting JSON-LD omitted %s: %s", want, got)
		}
	}
}

func TestCanonical(t *testing.T) {
	previous := baseURL
	t.Cleanup(func() { baseURL = previous })

	SetBaseURL("https://example.test///")
	if got, want := canonical("/about"), "https://example.test/about"; got != want {
		t.Fatalf("canonical(/about) = %q; want %q", got, want)
	}
	if got := canonical(""); got != "" {
		t.Fatalf("canonical(empty) = %q; want empty", got)
	}
}
