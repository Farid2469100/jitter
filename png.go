package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"golang.org/x/image/vector"
)

// renderPNG rasterizes jittered glyph data to a transparent PNG.
//
// It uses the same coordinate transform as renderSVG: font coordinate space
// (Y-up) -> pixel coordinate space (Y-down).
func renderPNG(glyphs []GlyphData, jitteredCommands [][]PathCommand, fontSize uint32, unitsPerEm uint16) ([]byte, error) {
	img, err := buildImage(glyphs, jitteredCommands, fontSize, unitsPerEm)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildImage rasterizes the jittered glyphs into an RGBA image with a
// transparent background and black glyphs.
func buildImage(glyphs []GlyphData, jitteredCommands [][]PathCommand, fontSize uint32, unitsPerEm uint16) (*image.NRGBA, error) {
	m := computeMetrics(glyphs, fontSize, unitsPerEm)

	// Pixmap dimensions must be non-zero.
	width := m.Width
	if width < 1 {
		width = 1
	}
	height := m.Height
	if height < 1 {
		height = 1
	}

	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	source := image.NewUniform(color.Alpha{A: 255})

	cursorX := 0.0

	for i, glyph := range glyphs {
		var commands []PathCommand
		if i < len(jitteredCommands) {
			commands = jitteredCommands[i]
		}
		if len(commands) == 0 {
			// Space or glyph with no outline: just advance.
			cursorX += glyph.AdvanceWidth
			continue
		}

		offsetX := cursorX * m.Scale

		// Rasterize each glyph into its own path. Filling all glyphs as one
		// combined path would let a counter (hole) in one glyph cancel an
		// overlapping neighbor under the non-zero winding rule; drawing them
		// separately unions their coverage, matching how fonts are painted.
		var rasterizer vector.Rasterizer
		rasterizer.Reset(width, height)

		for _, cmd := range commands {
			switch cmd.Op {
			case opMoveTo:
				sx, sy := transformPoint(cmd.X, cmd.Y, m.Scale, offsetX, m.BaselineY)
				rasterizer.MoveTo(float32(sx), float32(sy))
			case opLineTo:
				sx, sy := transformPoint(cmd.X, cmd.Y, m.Scale, offsetX, m.BaselineY)
				rasterizer.LineTo(float32(sx), float32(sy))
			case opQuadTo:
				scx, scy := transformPoint(cmd.CX1, cmd.CY1, m.Scale, offsetX, m.BaselineY)
				sx, sy := transformPoint(cmd.X, cmd.Y, m.Scale, offsetX, m.BaselineY)
				rasterizer.QuadTo(float32(scx), float32(scy), float32(sx), float32(sy))
			case opCurveTo:
				scx0, scy0 := transformPoint(cmd.CX1, cmd.CY1, m.Scale, offsetX, m.BaselineY)
				scx1, scy1 := transformPoint(cmd.CX2, cmd.CY2, m.Scale, offsetX, m.BaselineY)
				sx, sy := transformPoint(cmd.X, cmd.Y, m.Scale, offsetX, m.BaselineY)
				rasterizer.CubeTo(
					float32(scx0), float32(scy0),
					float32(scx1), float32(scy1),
					float32(sx), float32(sy),
				)
			case opClose:
				rasterizer.ClosePath()
			}
		}

		rasterizer.Draw(mask, mask.Bounds(), source, image.Point{})

		cursorX += glyph.AdvanceWidth
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := mask.AlphaAt(x, y).A
			if a == 0 {
				continue
			}
			i := img.PixOffset(x, y)
			// Black glyph with the rasterized coverage as alpha.
			img.Pix[i+0] = 0
			img.Pix[i+1] = 0
			img.Pix[i+2] = 0
			img.Pix[i+3] = a
		}
	}

	return img, nil
}
