package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

// PathOp identifies the kind of a single path command.
type PathOp uint8

const (
	opMoveTo PathOp = iota
	opLineTo
	opQuadTo
	opCurveTo
	opClose
)

// PathCommand is one outline command. Coordinates are in font units.
//
// For opQuadTo, (CX1, CY1) is the control point and (X, Y) the endpoint.
// For opCurveTo, (CX1, CY1) and (CX2, CY2) are the control points and
// (X, Y) the endpoint.
type PathCommand struct {
	Op       PathOp
	X, Y     float64
	CX1, CY1 float64
	CX2, CY2 float64
}

func moveTo(x, y float64) PathCommand { return PathCommand{Op: opMoveTo, X: x, Y: y} }
func lineTo(x, y float64) PathCommand { return PathCommand{Op: opLineTo, X: x, Y: y} }
func quadTo(cx, cy, x, y float64) PathCommand {
	return PathCommand{Op: opQuadTo, CX1: cx, CY1: cy, X: x, Y: y}
}
func curveTo(cx1, cy1, cx2, cy2, x, y float64) PathCommand {
	return PathCommand{Op: opCurveTo, CX1: cx1, CY1: cy1, CX2: cx2, CY2: cy2, X: x, Y: y}
}
func closePath() PathCommand { return PathCommand{Op: opClose} }

// GlyphData is a glyph outline plus its horizontal advance width.
type GlyphData struct {
	Commands     []PathCommand
	AdvanceWidth float64
}

// loadGlyphs reads a font file and extracts the outline and advance width for
// every character in text. It returns the glyphs and the font's units per em.
//
// Font collections (.ttc/.otc) are supported; the first font is used.
func loadGlyphs(fontPath, text string) ([]GlyphData, uint16, error) {
	data, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, 0, fmt.Errorf("Failed to read font file: %w", err)
	}

	faces, err := font.ParseTTC(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("Failed to parse font: %w", err)
	}
	if len(faces) == 0 {
		return nil, 0, fmt.Errorf("font contains no faces")
	}
	face := faces[0]

	unitsPerEm := face.Upem()
	glyphs := make([]GlyphData, 0, len([]rune(text)))

	for _, ch := range text {
		gid, ok := face.NominalGlyph(ch)
		if !ok {
			return nil, 0, fmt.Errorf("Character '%c' not found in font", ch)
		}

		outline, _ := face.GlyphDataOutline(gid)
		commands := segmentsToCommands(outline.Segments)

		glyphs = append(glyphs, GlyphData{
			Commands:     commands,
			AdvanceWidth: float64(face.HorizontalAdvance(gid)),
		})
	}

	return glyphs, unitsPerEm, nil
}

// segmentsToCommands converts go-text's outline segments into our command
// representation. Every contour is closed explicitly so downstream code can
// treat MoveTo/Close as contour boundaries.
func segmentsToCommands(segments []ot.Segment) []PathCommand {
	if len(segments) == 0 {
		return nil
	}

	commands := make([]PathCommand, 0, len(segments)+4)
	open := false

	for i := range segments {
		seg := &segments[i]
		switch seg.Op {
		case ot.SegmentOpMoveTo:
			if open {
				commands = append(commands, closePath())
			}
			p := seg.Args[0]
			commands = append(commands, moveTo(float64(p.X), float64(p.Y)))
			open = true
		case ot.SegmentOpLineTo:
			p := seg.Args[0]
			commands = append(commands, lineTo(float64(p.X), float64(p.Y)))
		case ot.SegmentOpQuadTo:
			c := seg.Args[0]
			p := seg.Args[1]
			commands = append(commands, quadTo(float64(c.X), float64(c.Y), float64(p.X), float64(p.Y)))
		case ot.SegmentOpCubeTo:
			c1 := seg.Args[0]
			c2 := seg.Args[1]
			p := seg.Args[2]
			commands = append(commands, curveTo(
				float64(c1.X), float64(c1.Y),
				float64(c2.X), float64(c2.Y),
				float64(p.X), float64(p.Y),
			))
		}
	}

	if open {
		commands = append(commands, closePath())
	}

	return commands
}
