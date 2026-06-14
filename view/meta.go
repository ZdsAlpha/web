package view

import "strings"

// baseURL is the site's absolute origin (e.g. "https://arehman.dev"), used to
// build canonical and og:url tags. Set once at startup via SetBaseURL before
// serving; empty means canonical tags are omitted.
var baseURL string

// SetBaseURL configures the absolute origin used for canonical URLs. The
// trailing slash is trimmed so canonical(path) can join cleanly.
func SetBaseURL(u string) { baseURL = strings.TrimRight(u, "/") }

// canonical joins the configured base URL with an absolute path. It returns ""
// when no base URL is configured or path is empty, which suppresses the tags.
func canonical(path string) string {
	if baseURL == "" || path == "" {
		return ""
	}
	return baseURL + path
}
