// Command genog regenerates static/og/default.png, the social-share (og:image)
// card. It is a build-time tool like tools/genchroma — run it and commit the
// PNG; the deployed binary only serves the committed file.
//
//	go run ./tools/genog
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Card dimensions: the universal 1.91:1 Open Graph size.
const (
	width  = 1200
	height = 630
	margin = 96
)

// Palette mirrors the site's dark theme (static/css/style.css).
var (
	bg     = color.RGBA{0x0e, 0x11, 0x16, 0xff}
	text   = color.RGBA{0xe6, 0xe8, 0xeb, 0xff}
	muted  = color.RGBA{0x9a, 0xa4, 0xb2, 0xff}
	accent = color.RGBA{0x6c, 0xa0, 0xff, 0xff}
)

func main() {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	// Accent bar above the title.
	draw.Draw(img, image.Rect(margin, 188, margin+88, 200), image.NewUniform(accent), image.Point{}, draw.Src)

	drawText(img, gobold.TTF, 104, text, margin, 320, "arehman.dev")
	drawText(img, goregular.TTF, 42, muted, margin, 392, "Notes on software, systems, and building things.")
	drawText(img, goregular.TTF, 34, accent, margin, height-margin, "Abdul Rahman")

	if err := os.MkdirAll("static/og", 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create("static/og/default.png")
	if err != nil {
		log.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatalf("encode: %v", err)
	}
	log.Printf("wrote static/og/default.png (%dx%d)", width, height)
}

// drawText renders s left-aligned with its baseline at (x, y) using the given
// embedded TTF at the given point size (DPI 72, so points map 1:1 to pixels).
func drawText(dst draw.Image, ttf []byte, size float64, c color.Color, x, y int, s string) {
	f, err := opentype.Parse(ttf)
	if err != nil {
		log.Fatalf("parse font: %v", err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		log.Fatalf("new face: %v", err)
	}
	defer face.Close()

	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}
