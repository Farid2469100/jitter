package main

import (
	crand "crypto/rand"
	"encoding/binary"
	"math"
	mrand "math/rand/v2"
	"os"
	"time"
)

// Upper bound on per-control-point offset, expressed as a fraction of
// unitsPerEm (multiplied by intensity). Keeps internal shake subtle relative
// to the rigid glyph translation (which uses 0.03).
const pointJitterScale = 0.01

// Upper bound on contour offset used for subtle stroke-weight variation,
// expressed as a fraction of unitsPerEm (multiplied by intensity). This sits
// below the rigid translation magnitude so glyphs feel "inked" differently
// rather than resized outright.
const strokeWeightScale = 0.012

// Maximum shear magnitude at intensity = 1.0, expressed as the tangent of the
// slant angle: tan(5°) ≈ 0.0875.
const shearMax = 0.08748866352592401

type point struct{ x, y float64 }
type normal = point

// floatSource is the subset of math/rand/v2.Rand used by the jitter engine.
type floatSource interface {
	Float64() float64
}

func uniform(r floatSource, min, max float64) float64 {
	return min + (max-min)*r.Float64()
}

func newSeededRNG(seed uint64) *mrand.Rand {
	var s [32]byte
	binary.LittleEndian.PutUint64(s[:8], seed)
	return mrand.New(mrand.NewChaCha8(s))
}

func newUnseededRNG() *mrand.Rand {
	var s [32]byte
	if _, err := crand.Read(s[:]); err != nil {
		binary.LittleEndian.PutUint64(s[:8], uint64(time.Now().UnixNano()))
		binary.LittleEndian.PutUint64(s[8:16], uint64(os.Getpid()))
	}
	return mrand.New(mrand.NewChaCha8(s))
}

// GlyphTransform holds the per-glyph random transformation parameters.
type GlyphTransform struct {
	angle  float64 // rotation angle in radians
	dx     float64 // X offset in font units
	dy     float64 // Y offset in font units
	scaleX float64 // scale factor along X
	scaleY float64 // scale factor along Y
	shearX float64 // shear along X (tan of slant angle): x' += shearX * y
	shearY float64 // shear along Y (tan of slant angle): y' += shearY * x
	cx     float64 // glyph center X (pivot)
	cy     float64 // glyph center Y (pivot)
}

// applyJitter applies jitter transformations to a list of path command sets
// (one per glyph). Each glyph gets a random rotation, anisotropic scale,
// shear, and position offset, followed by independent per-point noise on every
// bezier control point.
//
// intensity (0.0-1.0) controls the magnitude of the variation. unitsPerEm is
// used to scale the position offset relative to glyph size. When seed is
// non-nil, a deterministic RNG is used so identical input produces identical
// output. When nil, a non-deterministic RNG is used.
func applyJitter(glyphCommands [][]PathCommand, intensity, unitsPerEm float64, seed *uint64) [][]PathCommand {
	if intensity == 0 {
		out := make([][]PathCommand, len(glyphCommands))
		for i, cmds := range glyphCommands {
			out[i] = cloneCommands(cmds)
		}
		return out
	}

	var rng *mrand.Rand
	if seed != nil {
		rng = newSeededRNG(*seed)
	} else {
		rng = newUnseededRNG()
	}
	return runWithRNG(rng, glyphCommands, intensity, unitsPerEm)
}

// applyJitterOne applies jitter to a single glyph's path commands.
func applyJitterOne(commands []PathCommand, intensity, unitsPerEm float64) []PathCommand {
	if intensity == 0 {
		return cloneCommands(commands)
	}
	rng := newUnseededRNG()
	out := runWithRNG(rng, [][]PathCommand{commands}, intensity, unitsPerEm)
	if len(out) == 0 {
		return nil
	}
	return out[0]
}

func runWithRNG(rng *mrand.Rand, glyphCommands [][]PathCommand, intensity, unitsPerEm float64) [][]PathCommand {
	out := make([][]PathCommand, 0, len(glyphCommands))
	for _, commands := range glyphCommands {
		cx, cy := computeCenter(commands)
		transform := GlyphTransform{
			angle:  uniform(rng, -5.0, 5.0) * math.Pi / 180.0 * intensity,
			dx:     uniform(rng, -1.0, 1.0) * unitsPerEm * 0.03 * intensity,
			dy:     uniform(rng, -1.0, 1.0) * unitsPerEm * 0.03 * intensity,
			scaleX: 1.0 + uniform(rng, -0.10, 0.10)*intensity,
			scaleY: 1.0 + uniform(rng, -0.10, 0.10)*intensity,
			shearX: uniform(rng, -1.0, 1.0) * shearMax * intensity,
			shearY: uniform(rng, -1.0, 1.0) * shearMax * intensity,
			cx:     cx,
			cy:     cy,
		}
		transformed := applyTransform(commands, &transform)
		weighted := applyStrokeWeightVariation(transformed, rng, intensity, unitsPerEm)
		out = append(out, applyPointJitter(weighted, rng, intensity, unitsPerEm))
	}
	return out
}

// applyPointJitter applies independent small offsets to every on-curve and
// off-curve control point in the path. This sits after the rigid glyph
// transform so each point shakes on top of the per-glyph rotation/scale/
// translation. Close commands pass through unchanged.
func applyPointJitter(commands []PathCommand, rng *mrand.Rand, intensity, unitsPerEm float64) []PathCommand {
	amp := unitsPerEm * pointJitterScale * intensity
	if amp == 0 {
		return cloneCommands(commands)
	}

	offset := func() float64 {
		return uniform(rng, -1.0, 1.0) * amp
	}

	out := make([]PathCommand, 0, len(commands))
	for _, cmd := range commands {
		switch cmd.Op {
		case opMoveTo:
			out = append(out, moveTo(cmd.X+offset(), cmd.Y+offset()))
		case opLineTo:
			out = append(out, lineTo(cmd.X+offset(), cmd.Y+offset()))
		case opQuadTo:
			out = append(out, quadTo(
				cmd.CX1+offset(), cmd.CY1+offset(),
				cmd.X+offset(), cmd.Y+offset(),
			))
		case opCurveTo:
			out = append(out, curveTo(
				cmd.CX1+offset(), cmd.CY1+offset(),
				cmd.CX2+offset(), cmd.CY2+offset(),
				cmd.X+offset(), cmd.Y+offset(),
			))
		case opClose:
			out = append(out, closePath())
		}
	}
	return out
}

// applyStrokeWeightVariation applies a contour-aware offset that makes the
// glyph look slightly thicker or thinner without simply scaling the whole
// shape. A single random delta is chosen per glyph; each contour then moves
// along its outward normal. For counters/holes (opposite winding), the same
// delta naturally moves points in the opposite geometric direction, which
// keeps "thicker" glyphs thickening both the outer shell and the inner cutouts
// together.
func applyStrokeWeightVariation(commands []PathCommand, rng *mrand.Rand, intensity, unitsPerEm float64) []PathCommand {
	if intensity == 0 || len(commands) == 0 {
		return cloneCommands(commands)
	}
	delta := uniform(rng, -1.0, 1.0) * unitsPerEm * strokeWeightScale * intensity
	return applyStrokeWeightDelta(commands, delta)
}

func applyStrokeWeightDelta(commands []PathCommand, rawDelta float64) []PathCommand {
	if rawDelta == 0 || len(commands) == 0 {
		return cloneCommands(commands)
	}

	contours := splitContours(commands)
	geometries := make([]ContourGeometry, len(contours))
	for i, contour := range contours {
		geometries[i] = newContourGeometry(contour)
	}

	out := make([]PathCommand, 0, len(commands))
	for index, contour := range contours {
		insideCount := 0
		for otherIndex, other := range geometries {
			if otherIndex != index && pointInPolygon(geometries[index].interiorProbe, other.onCurvePoints) {
				insideCount++
			}
		}
		isHole := insideCount%2 == 1
		out = append(out, offsetContour(contour, &geometries[index], rawDelta, isHole)...)
	}
	return out
}

func splitContours(commands []PathCommand) [][]PathCommand {
	var contours [][]PathCommand
	var current []PathCommand

	for _, command := range commands {
		if command.Op == opMoveTo && len(current) > 0 {
			contours = append(contours, current)
			current = nil
		}
		current = append(current, command)
	}

	if len(current) > 0 {
		contours = append(contours, current)
	}

	return contours
}

type ContourGeometry struct {
	onCurvePoints []point
	area          float64
	interiorProbe point
}

func newContourGeometry(contour []PathCommand) ContourGeometry {
	points := onCurvePoints(contour)
	area := signedArea(points)
	return ContourGeometry{
		onCurvePoints: points,
		area:          area,
		interiorProbe: interiorProbe(points, area),
	}
}

func offsetContour(contour []PathCommand, geometry *ContourGeometry, rawDelta float64, isHole bool) []PathCommand {
	if len(geometry.onCurvePoints) < 2 {
		return cloneCommands(contour)
	}

	minX, minY, maxX, maxY := pointBounds(geometry.onCurvePoints)
	maxDelta := math.Max((math.Min(maxX-minX, maxY-minY))*0.12, 0.0)
	delta := math.Max(-maxDelta, math.Min(rawDelta, maxDelta))
	if delta == 0 {
		return cloneCommands(contour)
	}

	centroid := point{(minX + maxX) * 0.5, (minY + maxY) * 0.5}
	outwardSign := 1.0
	if geometry.area >= 0.0 {
		outwardSign = -1.0
	}
	fillSign := 1.0
	if isHole {
		fillSign = -1.0
	}
	effectiveDelta := outwardSign * fillSign * delta

	adjustedOnCurve, normals := offsetOnCurvePoints(geometry.onCurvePoints, centroid, effectiveDelta)
	return rebuildContour(contour, adjustedOnCurve, normals, effectiveDelta)
}

func onCurvePoints(contour []PathCommand) []point {
	points := make([]point, 0, len(contour))
	for _, command := range contour {
		switch command.Op {
		case opMoveTo, opLineTo, opQuadTo, opCurveTo:
			points = append(points, point{command.X, command.Y})
		case opClose:
		}
	}
	return points
}

func pointBounds(points []point) (minX, minY, maxX, maxY float64) {
	minX = math.Inf(1)
	minY = math.Inf(1)
	maxX = math.Inf(-1)
	maxY = math.Inf(-1)

	for _, p := range points {
		minX = math.Min(minX, p.x)
		minY = math.Min(minY, p.y)
		maxX = math.Max(maxX, p.x)
		maxY = math.Max(maxY, p.y)
	}

	return minX, minY, maxX, maxY
}

func signedArea(points []point) float64 {
	if len(points) < 3 {
		return 0.0
	}

	twiceArea := 0.0
	for i := range points {
		p0 := points[i]
		p1 := points[(i+1)%len(points)]
		twiceArea += p0.x*p1.y - p1.x*p0.y
	}
	return twiceArea * 0.5
}

func offsetOnCurvePoints(points []point, centroid point, delta float64) ([]point, []normal) {
	normals := make([]normal, len(points))
	for i := range points {
		prev := points[(i+len(points)-1)%len(points)]
		curr := points[i]
		next := points[(i+1)%len(points)]
		normals[i] = averagedOutwardNormal(prev, curr, next, centroid)
	}

	adjusted := make([]point, len(points))
	for i, p := range points {
		n := normals[i]
		adjusted[i] = point{p.x + n.x*delta, p.y + n.y*delta}
	}
	return adjusted, normals
}

func averagedOutwardNormal(prev, curr, next, centroid point) normal {
	var nx, ny float64

	if x, y, ok := edgeLeftNormal(curr.x-prev.x, curr.y-prev.y); ok {
		nx += x
		ny += y
	}
	if x, y, ok := edgeLeftNormal(next.x-curr.x, next.y-curr.y); ok {
		nx += x
		ny += y
	}

	if n, ok := normalize(point{nx, ny}); ok {
		return n
	}
	if x, y, ok := edgeLeftNormal(next.x-prev.x, next.y-prev.y); ok {
		return point{x, y}
	}
	if n, ok := normalize(point{curr.x - centroid.x, curr.y - centroid.y}); ok {
		return n
	}
	return point{}
}

func edgeLeftNormal(dx, dy float64) (float64, float64, bool) {
	n, ok := normalize(point{-dy, dx})
	return n.x, n.y, ok
}

func normalize(p point) (point, bool) {
	const f64Epsilon = 2.220446049250313e-16 // math.Nextafter(1, 2) - 1
	length := math.Sqrt(p.x*p.x + p.y*p.y)
	if length <= f64Epsilon {
		return point{}, false
	}
	return point{p.x / length, p.y / length}, true
}

func interiorProbe(points []point, area float64) point {
	if len(points) == 0 {
		return point{}
	}
	if len(points) == 1 {
		return points[0]
	}

	minX, minY, maxX, maxY := pointBounds(points)
	epsilon := math.Max(math.Max(maxX-minX, maxY-minY)*1e-3, 1e-3)

	for i := range points {
		curr := points[i]
		next := points[(i+1)%len(points)]
		dx := next.x - curr.x
		dy := next.y - curr.y
		if leftX, leftY, ok := edgeLeftNormal(dx, dy); ok {
			interiorSign := -1.0
			if area >= 0.0 {
				interiorSign = 1.0
			}
			midpoint := point{(curr.x + next.x) * 0.5, (curr.y + next.y) * 0.5}
			return point{
				midpoint.x + leftX*interiorSign*epsilon,
				midpoint.y + leftY*interiorSign*epsilon,
			}
		}
	}

	return points[0]
}

func pointInPolygon(p point, polygon []point) bool {
	if len(polygon) < 3 {
		return false
	}

	inside := false
	prev := polygon[len(polygon)-1]
	for _, curr := range polygon {
		intersects := ((curr.y > p.y) != (prev.y > p.y)) &&
			(p.x < (prev.x-curr.x)*(p.y-curr.y)/(prev.y-curr.y)+curr.x)
		if intersects {
			inside = !inside
		}
		prev = curr
	}
	return inside
}

func rebuildContour(contour []PathCommand, adjustedOnCurve []point, normals []normal, delta float64) []PathCommand {
	if len(adjustedOnCurve) == 0 || len(normals) != len(adjustedOnCurve) {
		return cloneCommands(contour)
	}
	if len(onCurvePoints(contour)) != len(adjustedOnCurve) {
		return cloneCommands(contour)
	}

	out := make([]PathCommand, 0, len(contour))
	onCurveIndex := 0

	for _, command := range contour {
		switch command.Op {
		case opMoveTo:
			p := adjustedOnCurve[onCurveIndex]
			out = append(out, moveTo(p.x, p.y))
		case opLineTo:
			onCurveIndex++
			p := adjustedOnCurve[onCurveIndex]
			out = append(out, lineTo(p.x, p.y))
		case opQuadTo:
			startIndex := onCurveIndex
			endIndex := (onCurveIndex + 1) % len(adjustedOnCurve)
			offsetX, offsetY := segmentControlOffset(normals[startIndex], normals[endIndex], delta)
			onCurveIndex = endIndex
			end := adjustedOnCurve[endIndex]
			out = append(out, quadTo(command.CX1+offsetX, command.CY1+offsetY, end.x, end.y))
		case opCurveTo:
			startIndex := onCurveIndex
			endIndex := (onCurveIndex + 1) % len(adjustedOnCurve)
			offsetX, offsetY := segmentControlOffset(normals[startIndex], normals[endIndex], delta)
			onCurveIndex = endIndex
			end := adjustedOnCurve[endIndex]
			out = append(out, curveTo(
				command.CX1+offsetX, command.CY1+offsetY,
				command.CX2+offsetX, command.CY2+offsetY,
				end.x, end.y,
			))
		case opClose:
			out = append(out, closePath())
		}
	}

	return out
}

func segmentControlOffset(startNormal, endNormal normal, delta float64) (float64, float64) {
	average, ok := normalize(point{startNormal.x + endNormal.x, startNormal.y + endNormal.y})
	if !ok {
		average = startNormal
	}
	return average.x * delta, average.y * delta
}

// computeCenter computes the bounding-box center of a set of path commands.
func computeCenter(commands []PathCommand) (float64, float64) {
	minX := math.Inf(1)
	minY := math.Inf(1)
	maxX := math.Inf(-1)
	maxY := math.Inf(-1)
	any := false

	update := func(x, y float64) {
		any = true
		minX = math.Min(minX, x)
		minY = math.Min(minY, y)
		maxX = math.Max(maxX, x)
		maxY = math.Max(maxY, y)
	}

	for _, cmd := range commands {
		switch cmd.Op {
		case opMoveTo, opLineTo:
			update(cmd.X, cmd.Y)
		case opQuadTo:
			update(cmd.CX1, cmd.CY1)
			update(cmd.X, cmd.Y)
		case opCurveTo:
			update(cmd.CX1, cmd.CY1)
			update(cmd.CX2, cmd.CY2)
			update(cmd.X, cmd.Y)
		case opClose:
		}
	}

	if !any {
		return 0.0, 0.0
	}
	return (minX + maxX) / 2.0, (minY + maxY) / 2.0
}

func applyTransform(commands []PathCommand, t *GlyphTransform) []PathCommand {
	out := make([]PathCommand, 0, len(commands))
	for _, cmd := range commands {
		switch cmd.Op {
		case opMoveTo:
			x, y := transformGlyphPoint(cmd.X, cmd.Y, t)
			out = append(out, moveTo(x, y))
		case opLineTo:
			x, y := transformGlyphPoint(cmd.X, cmd.Y, t)
			out = append(out, lineTo(x, y))
		case opQuadTo:
			cx, cy := transformGlyphPoint(cmd.CX1, cmd.CY1, t)
			x, y := transformGlyphPoint(cmd.X, cmd.Y, t)
			out = append(out, quadTo(cx, cy, x, y))
		case opCurveTo:
			cx1, cy1 := transformGlyphPoint(cmd.CX1, cmd.CY1, t)
			cx2, cy2 := transformGlyphPoint(cmd.CX2, cmd.CY2, t)
			x, y := transformGlyphPoint(cmd.X, cmd.Y, t)
			out = append(out, curveTo(cx1, cy1, cx2, cy2, x, y))
		case opClose:
			out = append(out, closePath())
		}
	}
	return out
}

// transformGlyphPoint applies anisotropic scale + shear, rotation (around the
// glyph center), and translation to a point.
func transformGlyphPoint(x, y float64, t *GlyphTransform) (float64, float64) {
	// Translate to glyph center.
	dx := x - t.cx
	dy := y - t.cy
	// 2x2 linear transform: anisotropic scale on the diagonal, shear on the
	// off-diagonal. Applied before rotation so rotation sees the stretched
	// glyph.
	sx := t.scaleX*dx + t.shearX*dy
	sy := t.shearY*dx + t.scaleY*dy
	// Rotate.
	cos := math.Cos(t.angle)
	sin := math.Sin(t.angle)
	rx := sx*cos - sy*sin
	ry := sx*sin + sy*cos
	// Translate back and apply position offset.
	return rx + t.cx + t.dx, ry + t.cy + t.dy
}

func cloneCommands(commands []PathCommand) []PathCommand {
	out := make([]PathCommand, len(commands))
	copy(out, commands)
	return out
}
