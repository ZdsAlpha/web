package content

import (
	"bytes"

	"github.com/alecthomas/chroma/v2/formatters/html"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"go.abhg.dev/goldmark/anchor"
)

// renderer wraps a configured goldmark instance.
type renderer struct {
	md goldmark.Markdown
}

func newRenderer() *renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,         // tables, strikethrough, autolinks, task lists
			extension.Footnote,    // footnotes
			extension.Typographer, // smart quotes / dashes
			highlighting.NewHighlighting(
				highlighting.WithStyle("github"),
				highlighting.WithFormatOptions(
					// Emit CSS classes instead of inline styles so the theme
					// lives in static/css/chroma.css and can swap for dark mode.
					html.WithClasses(true),
				),
			),
			&anchor.Extender{
				Texter:   anchor.Text("#"),
				Position: anchor.Before,
			},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(), // deterministic heading IDs for anchors
		),
		// Note: WithUnsafe() is intentionally NOT set. goldmark escapes raw
		// HTML by default, which is the safe choice for authored content.
	)
	return &renderer{md: md}
}

// render converts markdown source to HTML.
func (r *renderer) render(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := r.md.Convert(src, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
