package main

import (
	"embed"
	"fmt"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/text"
)

// The app's text is set in Lato, the typeface Slack's interface is based
// on, and code in DejaVu Sans Mono, which Menlo, the Mac's monospace font,
// is based on. Both are embedded, because Gio draws no text in Menlo
// itself; their licenses are in fonts/.
//
//go:embed fonts/*.ttf
var fontFiles embed.FS

const (
	sans = "Lato"
	mono = "DejaVu Sans Mono" // also for symbols such as ✓, which Lato lacks
)

// fonts returns the embedded fonts, and the Go fonts as a fallback.
func fonts() []font.FontFace {
	entries, err := fontFiles.ReadDir("fonts")
	if err != nil {
		panic(err)
	}
	var faces []font.FontFace
	for _, e := range entries {
		data, err := fontFiles.ReadFile("fonts/" + e.Name())
		if err != nil {
			panic(err)
		}
		fs, err := opentype.ParseCollection(data)
		if err != nil {
			panic(fmt.Sprintf("parsing %s: %v", e.Name(), err))
		}
		faces = append(faces, fs...)
	}
	return append(faces, gofont.Collection()...)
}

// newShaper returns a shaper with the app's fonts and the system's, which
// cover scripts the app's fonts lack.
func newShaper() *text.Shaper {
	return text.NewShaper(text.WithCollection(fonts()))
}
