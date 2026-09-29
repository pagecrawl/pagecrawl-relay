// Draws the menu-bar icons, one per relay state, into the package root:
//
//	go run ./packaging/macos/trayicon
//
// They are macOS template images: pure black with an alpha channel, which is how
// a menu-bar icon is meant to be supplied. macOS recolours a template itself, so
// one file per state covers light and dark menu bars, an inactive Space and the
// highlighted state when the menu is open. Nothing here is hand-drawn, so the set
// stays consistent: every state shares the same ring and differs only inside it.
//
// 32x32, because systray draws the image at 16 points: at 16x16 the icon is soft
// on every Mac sold in a decade.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const (
	size    = 32
	samples = 8 // supersampling per axis, for antialiased edges

	centre     = size / 2.0
	ringOuter  = 13.0
	ringInner  = 10.3
	dotRadius  = 5.4
	barHalfW   = 1.45
	barHalfH   = 5.4
	barOffsetX = 3.2
)

// A shape reports whether a point is inside it. Coordinates are in pixels, with
// the icon's centre at (centre, centre).
type shape func(x, y float64) bool

func ring(x, y float64) bool {
	d := math.Hypot(x-centre, y-centre)

	return d <= ringOuter && d >= ringInner
}

// relaying: a filled centre, the state someone wants to confirm at a glance.
func dot(x, y float64) bool {
	return math.Hypot(x-centre, y-centre) <= dotRadius
}

// paused: the universal two bars, so the state reads without a legend.
func bars(x, y float64) bool {
	dy := math.Abs(y - centre)
	if dy > barHalfH {
		return false
	}

	dx := math.Abs(x - centre)

	return dx >= barOffsetX-barHalfW && dx <= barOffsetX+barHalfW
}

func union(shapes ...shape) shape {
	return func(x, y float64) bool {
		for _, s := range shapes {
			if s(x, y) {
				return true
			}
		}

		return false
	}
}

func render(s shape) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			hits := 0

			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					x := float64(px) + (float64(sx)+0.5)/samples
					y := float64(py) + (float64(sy)+0.5)/samples

					if s(x, y) {
						hits++
					}
				}
			}

			if hits == 0 {
				continue
			}

			// Black plus coverage: the template's alpha is the whole picture,
			// because macOS replaces the colour.
			alpha := uint8(math.Round(255 * float64(hits) / float64(samples*samples)))
			img.SetNRGBA(px, py, color.NRGBA{A: alpha})
		}
	}

	return img
}

func main() {
	// Written beside the Go files that embed them, whatever the working directory.
	root, err := filepath.Abs(filepath.Join("packaging", "macos", "trayicon", "..", "..", ".."))
	if err != nil {
		log.Fatal(err)
	}

	icons := []struct {
		name  string
		shape shape
	}{
		{"icon.png", union(ring, dot)},
		{"icon-paused.png", union(ring, bars)},
		// Not connected: the ring alone, an outline that reads as empty rather
		// than as a second working state.
		{"icon-offline.png", ring},
	}

	for _, icon := range icons {
		path := filepath.Join(root, icon.name)

		f, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}

		if err := png.Encode(f, render(icon.shape)); err != nil {
			log.Fatal(err)
		}

		if err := f.Close(); err != nil {
			log.Fatal(err)
		}

		log.Printf("wrote %s", icon.name)
	}
}
