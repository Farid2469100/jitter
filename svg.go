package main

import (
	"fmt"
	"strings"
)

// renderSVG generates an SVG document from jittered glyph data.
//
// It converts from font coordinate space (Y-up) to SVG coordinate space
// (Y-down).
func renderSVG(glyphs []GlyphData, jitteredCommands [][]PathCommand, fontSize uint32, unitsPerEm uint16) string {
	m := computeMetrics(glyphs, fontSize, unitsPerEm)

	var svg strings.Builder
	fmt.Fprintf(
		&svg,
		"<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 %d %d\" width=\"%d\" height=\"%d\">\n",
		m.Width, m.Height, m.Width, m.Height,
	)

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

		pathD := commandsToPathD(commands, m.Scale, cursorX*m.Scale, m.BaselineY)
		if pathD != "" {
			fmt.Fprintf(&svg, "  <path id=\"g%d\" d=\"%s\" fill=\"black\"/>\n", i, pathD)
		}

		cursorX += glyph.AdvanceWidth
	}

	svg.WriteString("</svg>\n")
	return svg.String()
}

// commandsToPathD converts path commands to an SVG path "d" attribute.
//
// Applies the coordinate transformation: font (Y-up) -> SVG (Y-down).
func commandsToPathD(commands []PathCommand, scale, offsetX, baselineY float64) string {
	var d strings.Builder

	for _, cmd := range commands {
		switch cmd.Op {
		case opMoveTo:
			sx, sy := transformPoint(cmd.X, cmd.Y, scale, offsetX, baselineY)
			fmt.Fprintf(&d, "M%.2f %.2f ", sx, sy)
		case opLineTo:
			sx, sy := transformPoint(cmd.X, cmd.Y, scale, offsetX, baselineY)
			fmt.Fprintf(&d, "L%.2f %.2f ", sx, sy)
		case opQuadTo:
			scx, scy := transformPoint(cmd.CX1, cmd.CY1, scale, offsetX, baselineY)
			sx, sy := transformPoint(cmd.X, cmd.Y, scale, offsetX, baselineY)
			fmt.Fprintf(&d, "Q%.2f %.2f %.2f %.2f ", scx, scy, sx, sy)
		case opCurveTo:
			scx0, scy0 := transformPoint(cmd.CX1, cmd.CY1, scale, offsetX, baselineY)
			scx1, scy1 := transformPoint(cmd.CX2, cmd.CY2, scale, offsetX, baselineY)
			sx, sy := transformPoint(cmd.X, cmd.Y, scale, offsetX, baselineY)
			fmt.Fprintf(&d, "C%.2f %.2f %.2f %.2f %.2f %.2f ", scx0, scy0, scx1, scy1, sx, sy)
		case opClose:
			d.WriteString("Z ")
		}
	}

	return strings.TrimRight(d.String(), " ")
}
