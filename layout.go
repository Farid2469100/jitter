package main

import "math"

// PageMetrics holds the canvas dimensions and the transform parameters used
// when converting font coordinates to page coordinates.
type PageMetrics struct {
	Width     int
	Height    int
	Scale     float64
	BaselineY float64
}

// computeMetrics derives canvas metrics from glyph advance widths and font size.
//
// It guards against unitsPerEm == 0 (broken fonts) by clamping to 1.
// The baseline sits at ~73% of the height (1.1 / 1.5).
func computeMetrics(glyphs []GlyphData, fontSize uint32, unitsPerEm uint16) PageMetrics {
	upem := unitsPerEm
	if upem == 0 {
		upem = 1
	}

	scale := float64(fontSize) / float64(upem)

	totalAdvance := 0.0
	for _, g := range glyphs {
		totalAdvance += g.AdvanceWidth
	}

	width := int(math.Ceil(totalAdvance * scale))
	// Use 1.5x font size for height to accommodate ascenders/descenders.
	height := int(math.Ceil(float64(fontSize) * 1.5))
	baselineY := float64(fontSize) * 1.1

	return PageMetrics{
		Width:     width,
		Height:    height,
		Scale:     scale,
		BaselineY: baselineY,
	}
}

// transformPoint maps a font-space point (Y-up) to canvas space (Y-down).
func transformPoint(x, y, scale, offsetX, baselineY float64) (float64, float64) {
	sx := x*scale + offsetX
	sy := -y*scale + baselineY
	return sx, sy
}
