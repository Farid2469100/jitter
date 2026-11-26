package main

import (
	"fmt"
	"math"
)

type ttPoint struct {
	x, y float64
	on   bool
}

type glyphInfo struct {
	empty       bool
	xMin, yMin  int16
	xMax, yMax  int16
	numPoints   uint16
	numContours uint16
}

// buildGlyf converts every glyph's commands into TrueType `glyf` entries and
// returns the table bytes plus the loca offsets (one extra trailing offset).
func buildGlyf(glyphs [][]PathCommand) (glyf []byte, offsets []uint32, infos []glyphInfo, err error) {
	offsets = make([]uint32, len(glyphs)+1)
	infos = make([]glyphInfo, len(glyphs))

	for i, commands := range glyphs {
		offsets[i] = uint32(len(glyf))

		data, info, err := buildSimpleGlyph(commands)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("glyph %d: %w", i, err)
		}
		glyf = append(glyf, data...)

		// Keep glyph offsets even so short loca can be used when possible.
		if len(glyf)%2 != 0 {
			glyf = append(glyf, 0)
		}

		infos[i] = info
	}

	offsets[len(glyphs)] = uint32(len(glyf))
	return glyf, offsets, infos, nil
}

// buildSimpleGlyph converts one outline into a TrueType simple glyph.
// Empty outlines produce an empty glyph (zero-length loca entry).
func buildSimpleGlyph(commands []PathCommand) ([]byte, glyphInfo, error) {
	contours := commandsToContours(commands)

	cleaned := contours[:0]
	for _, contour := range contours {
		contour = normalizeContour(contour)
		if len(contour) >= 2 {
			cleaned = append(cleaned, contour)
		}
	}
	contours = cleaned

	if len(contours) == 0 {
		return nil, glyphInfo{empty: true}, nil
	}
	if len(contours) > math.MaxInt16 {
		return nil, glyphInfo{}, fmt.Errorf("too many contours: %d", len(contours))
	}

	// Round to integer font units, clamp to the int16 coordinate range, and
	// drop contours that collapse to a single point.
	roundedContours := make([][]ttPoint, 0, len(contours))
	minX, minY := int32(math.MaxInt32), int32(math.MaxInt32)
	maxX, maxY := int32(math.MinInt32), int32(math.MinInt32)

	for _, contour := range contours {
		rounded := make([]ttPoint, len(contour))
		for i, p := range contour {
			x := int32(clampInt(int64(math.Round(p.x)), -32768, 32767))
			y := int32(clampInt(int64(math.Round(p.y)), -32768, 32767))
			rounded[i] = ttPoint{float64(x), float64(y), p.on}
		}

		// If rounding made the closing point identical to the start, drop it.
		if len(rounded) >= 2 {
			first := rounded[0]
			last := rounded[len(rounded)-1]
			if last.on && first.on && last.x == first.x && last.y == first.y {
				rounded = rounded[:len(rounded)-1]
			}
		}
		if len(rounded) < 2 {
			continue
		}

		for _, p := range rounded {
			x := int32(p.x)
			y := int32(p.y)
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
		roundedContours = append(roundedContours, rounded)
	}

	if len(roundedContours) == 0 {
		return nil, glyphInfo{empty: true}, nil
	}

	var points []ttPoint
	endPts := make([]uint16, 0, len(roundedContours))
	for _, contour := range roundedContours {
		points = append(points, contour...)
		if len(points) > math.MaxUint16 {
			return nil, glyphInfo{}, fmt.Errorf("too many points: %d", len(points))
		}
		endPts = append(endPts, uint16(len(points)-1))
	}

	info := glyphInfo{
		xMin:        int16(minX),
		yMin:        int16(minY),
		xMax:        int16(maxX),
		yMax:        int16(maxY),
		numPoints:   uint16(len(points)),
		numContours: uint16(len(roundedContours)),
	}

	out := make([]byte, 0, 16+2*len(endPts)+len(points)+4*len(points))
	out = appendI16(out, int16(len(roundedContours)))
	out = appendI16(out, info.xMin)
	out = appendI16(out, info.yMin)
	out = appendI16(out, info.xMax)
	out = appendI16(out, info.yMax)
	for _, end := range endPts {
		out = appendU16(out, end)
	}
	out = appendU16(out, 0) // instructionLength

	flags := make([]byte, len(points))
	xData := make([]byte, 0, len(points))
	yData := make([]byte, 0, len(points))

	prevX, prevY := int32(0), int32(0)
	for i, p := range points {
		x := int32(p.x)
		y := int32(p.y)
		dx := x - prevX
		dy := y - prevY

		flag := byte(0)
		if p.on {
			flag |= 0x01
		}

		if dx == 0 {
			flag |= 0x10
		} else if dx >= -255 && dx <= 255 {
			flag |= 0x02
			if dx > 0 {
				flag |= 0x10
			}
			xData = append(xData, byte(absInt32(dx)))
		} else {
			if dx < math.MinInt16 || dx > math.MaxInt16 {
				return nil, glyphInfo{}, fmt.Errorf("x delta %d does not fit in int16", dx)
			}
			xData = appendI16(xData, int16(dx))
		}

		if dy == 0 {
			flag |= 0x20
		} else if dy >= -255 && dy <= 255 {
			flag |= 0x04
			if dy > 0 {
				flag |= 0x20
			}
			yData = append(yData, byte(absInt32(dy)))
		} else {
			if dy < math.MinInt16 || dy > math.MaxInt16 {
				return nil, glyphInfo{}, fmt.Errorf("y delta %d does not fit in int16", dy)
			}
			yData = appendI16(yData, int16(dy))
		}

		flags[i] = flag
		prevX, prevY = x, y
	}

	out = append(out, flags...)
	out = append(out, xData...)
	out = append(out, yData...)

	return out, info, nil
}

func commandsToContours(commands []PathCommand) [][]ttPoint {
	var contours [][]ttPoint
	var current []ttPoint
	open := false

	flush := func() {
		if open {
			contours = append(contours, current)
			current = nil
			open = false
		}
	}

	for _, cmd := range commands {
		switch cmd.Op {
		case opMoveTo:
			flush()
			current = []ttPoint{{cmd.X, cmd.Y, true}}
			open = true
		case opLineTo:
			if !open {
				continue
			}
			current = append(current, ttPoint{cmd.X, cmd.Y, true})
		case opQuadTo:
			if !open {
				continue
			}
			current = append(current,
				ttPoint{cmd.CX1, cmd.CY1, false},
				ttPoint{cmd.X, cmd.Y, true},
			)
		case opCurveTo:
			if !open || len(current) == 0 {
				continue
			}
			start := current[len(current)-1]
			p0 := point{start.x, start.y}
			p1 := point{cmd.CX1, cmd.CY1}
			p2 := point{cmd.CX2, cmd.CY2}
			p3 := point{cmd.X, cmd.Y}
			for _, q := range cubicToQuads(p0, p1, p2, p3, 0.5) {
				current = append(current,
					ttPoint{q.cx, q.cy, false},
					ttPoint{q.x, q.y, true},
				)
			}
		case opClose:
			flush()
		}
	}
	flush()

	return contours
}

func normalizeContour(contour []ttPoint) []ttPoint {
	if len(contour) < 2 {
		return nil
	}

	// Drop a trailing explicit duplicate of the first on-curve point.
	first := contour[0]
	last := contour[len(contour)-1]
	if last.on && first.on && samePoint(last, first) {
		contour = contour[:len(contour)-1]
	}
	if len(contour) < 2 {
		return nil
	}

	// Rotate so the contour starts on an on-curve point.
	start := -1
	for i, p := range contour {
		if p.on {
			start = i
			break
		}
	}

	if start < 0 {
		// All points are off-curve; insert an implied on-curve midpoint.
		mid := ttPoint{
			x:  (contour[len(contour)-1].x + contour[0].x) / 2,
			y:  (contour[len(contour)-1].y + contour[0].y) / 2,
			on: true,
		}
		contour = append([]ttPoint{mid}, contour...)
	} else if start > 0 {
		rotated := make([]ttPoint, 0, len(contour))
		rotated = append(rotated, contour[start:]...)
		rotated = append(rotated, contour[:start]...)
		contour = rotated
	}

	// TrueType allows consecutive off-curve points (an implied on-curve point
	// sits between them), but emitting the midpoint explicitly is simpler and
	// universally accepted.
	out := make([]ttPoint, 0, len(contour)+2)
	for i, p := range contour {
		out = append(out, p)
		next := contour[(i+1)%len(contour)]
		if !p.on && !next.on {
			out = append(out, ttPoint{
				x:  (p.x + next.x) / 2,
				y:  (p.y + next.y) / 2,
				on: true,
			})
		}
	}
	contour = out

	// Drop a trailing duplicate introduced by the midpoint insertion.
	if len(contour) >= 2 {
		first = contour[0]
		last = contour[len(contour)-1]
		if last.on && first.on && samePoint(last, first) {
			contour = contour[:len(contour)-1]
		}
	}

	if len(contour) < 2 {
		return nil
	}
	return contour
}

func samePoint(a, b ttPoint) bool {
	const eps = 1e-6
	return math.Abs(a.x-b.x) <= eps && math.Abs(a.y-b.y) <= eps
}

type quadSeg struct {
	cx, cy float64
	x, y   float64
}

// cubicToQuads approximates a cubic Bezier with quadratic segments. It
// recursively splits the cubic until the quadratic approximation is within
// tolerance font units.
func cubicToQuads(p0, p1, p2, p3 point, tolerance float64) []quadSeg {
	if tolerance <= 0 {
		tolerance = 0.5
	}

	var out []quadSeg
	var recurse func(p0, p1, p2, p3 point, depth int)
	recurse = func(p0, p1, p2, p3 point, depth int) {
		q := point{
			x: (3*p1.x + 3*p2.x - p0.x - p3.x) / 4,
			y: (3*p1.y + 3*p2.y - p0.y - p3.y) / 4,
		}

		if depth >= 16 || cubicQuadError(p0, q, p3, p1, p2) <= tolerance {
			out = append(out, quadSeg{cx: q.x, cy: q.y, x: p3.x, y: p3.y})
			return
		}

		p01 := midpoint(p0, p1)
		p12 := midpoint(p1, p2)
		p23 := midpoint(p2, p3)
		p012 := midpoint(p01, p12)
		p123 := midpoint(p12, p23)
		p0123 := midpoint(p012, p123)

		recurse(p0, p01, p012, p0123, depth+1)
		recurse(p0123, p123, p23, p3, depth+1)
	}
	recurse(p0, p1, p2, p3, 0)
	return out
}

func cubicQuadError(p0, q, p3, c1, c2 point) float64 {
	maxErr := 0.0
	for _, t := range []float64{0.125, 0.25, 0.5, 0.75, 0.875} {
		c := cubicPoint(p0, c1, c2, p3, t)
		b := quadPoint(p0, q, p3, t)
		dx := c.x - b.x
		dy := c.y - b.y
		if d := math.Sqrt(dx*dx + dy*dy); d > maxErr {
			maxErr = d
		}
	}
	return maxErr
}

func cubicPoint(p0, p1, p2, p3 point, t float64) point {
	mt := 1 - t
	a := mt * mt * mt
	b := 3 * mt * mt * t
	c := 3 * mt * t * t
	d := t * t * t
	return point{
		x: a*p0.x + b*p1.x + c*p2.x + d*p3.x,
		y: a*p0.y + b*p1.y + c*p2.y + d*p3.y,
	}
}

func quadPoint(p0, q, p3 point, t float64) point {
	mt := 1 - t
	a := mt * mt
	b := 2 * mt * t
	c := t * t
	return point{
		x: a*p0.x + b*q.x + c*p3.x,
		y: a*p0.y + b*q.y + c*p3.y,
	}
}

func midpoint(a, b point) point {
	return point{x: (a.x + b.x) / 2, y: (a.y + b.y) / 2}
}

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func appendU16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func appendI16(b []byte, v int16) []byte {
	return appendU16(b, uint16(v))
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt(v int64, min, max int64) int64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
