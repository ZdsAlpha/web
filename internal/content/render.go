package content

import (
	"bytes"

	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
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
			parser.WithASTTransformers(
				util.Prioritized(imageAttributeTransformer{}, 100),
			),
		),
		// Note: WithUnsafe() is intentionally NOT set. goldmark escapes raw
		// HTML by default, which is the safe choice for authored content.
	)
	return &renderer{md: md}
}

// imageAttributeTransformer keeps authored Markdown terse while ensuring
// below-the-fold article images do not block initial rendering or decoding.
type imageAttributeTransformer struct{}

func (imageAttributeTransformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == ast.KindImage {
			n.SetAttributeString("loading", "lazy")
			n.SetAttributeString("decoding", "async")
		}
		return ast.WalkContinue, nil
	})
}

// render converts markdown source to HTML.
func (r *renderer) render(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := r.md.Convert(src, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
