package main

import (
	"math"
	"testing"
)

func sampleInput() [][]PathCommand {
	return [][]PathCommand{
		{
			moveTo(0.0, 0.0),
			lineTo(100.0, 100.0),
			quadTo(50.0, 150.0, 100.0, 200.0),
			curveTo(10.0, 20.0, 30.0, 40.0, 50.0, 60.0),
			closePath(),
		},
		{
			moveTo(200.0, 200.0),
			lineTo(300.0, 250.0),
			closePath(),
		},
	}
}

func seed(v uint64) *uint64 { return &v }

func TestSameSeedProducesIdenticalOutput(t *testing.T) {
	input := sampleInput()
	a := applyJitter(input, 0.7, 1000.0, seed(42))
	b := applyJitter(input, 0.7, 1000.0, seed(42))
	if !commandsEqual(a, b) {
		t.Fatal("same seed must produce identical output")
	}
}

func TestDifferentSeedsProduceDifferentOutput(t *testing.T) {
	input := sampleInput()
	a := applyJitter(input, 0.7, 1000.0, seed(1))
	b := applyJitter(input, 0.7, 1000.0, seed(2))
	if commandsEqual(a, b) {
		t.Fatal("different seeds must produce different output")
	}
}

func TestNoSeedStillVaries(t *testing.T) {
	input := sampleInput()
	out := applyJitter(input, 0.7, 1000.0, nil)
	if len(out) != len(input) {
		t.Fatalf("expected %d glyphs, got %d", len(input), len(out))
	}
	if commandsEqual(out, input) {
		t.Fatal("intensity > 0 should change the output")
	}
}

func TestIntensityZeroIsIdentityRegardlessOfSeed(t *testing.T) {
	input := sampleInput()
	a := applyJitter(input, 0.0, 1000.0, seed(1))
	b := applyJitter(input, 0.0, 1000.0, seed(9999))
	if !commandsEqual(a, b) {
		t.Fatal("intensity 0 output must not depend on the seed")
	}
	if !commandsEqual(a, input) {
		t.Fatal("intensity 0 must be an exact identity")
	}
}

// With intensity > 0, per-point jitter should break the rigid-body invariant:
// the transformed points cannot all be explained by a single affine transform
// applied to the input.
func TestPerPointJitterVariesInternalShape(t *testing.T) {
	input := [][]PathCommand{{
		moveTo(0.0, 0.0),
		lineTo(100.0, 0.0),
		lineTo(200.0, 0.0),
		lineTo(300.0, 0.0),
		lineTo(400.0, 0.0),
		closePath(),
	}}

	out := applyJitter(input, 1.0, 1000.0, seed(42))
	if len(out) != 1 {
		t.Fatalf("expected 1 glyph, got %d", len(out))
	}

	var points []point
	for _, cmd := range out[0] {
		if cmd.Op == opMoveTo || cmd.Op == opLineTo {
			points = append(points, point{cmd.X, cmd.Y})
		}
	}
	if len(points) != 5 {
		t.Fatalf("expected 5 points, got %d", len(points))
	}

	x0, y0 := points[0].x, points[0].y
	dx := points[1].x - x0
	dy := points[1].y - y0
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		t.Fatal("first two points must not coincide")
	}

	maxOffLine := 0.0
	for _, p := range points[2:] {
		off := math.Abs((p.x-x0)*dy-(p.y-y0)*dx) / length
		if off > maxOffLine {
			maxOffLine = off
		}
	}
	if maxOffLine <= 0.5 {
		t.Fatalf("per-point jitter should break collinearity (off-line = %v)", maxOffLine)
	}
}

func TestApplyTransformAppliesAnisotropicScale(t *testing.T) {
	tr := GlyphTransform{
		scaleX: 2.0,
		scaleY: 1.0,
	}
	out := applyTransform([]PathCommand{moveTo(1.0, 1.0)}, &tr)
	if out[0].Op != opMoveTo {
		t.Fatalf("expected MoveTo, got %v", out[0].Op)
	}
	if math.Abs(out[0].X-2.0) > 1e-5 {
		t.Fatalf("x should be 2.0, got %v", out[0].X)
	}
	if math.Abs(out[0].Y-1.0) > 1e-5 {
		t.Fatalf("y should be 1.0, got %v", out[0].Y)
	}
}

func TestApplyTransformAppliesShear(t *testing.T) {
	tr := GlyphTransform{
		scaleX: 1.0,
		scaleY: 1.0,
		shearX: 0.5,
	}
	out := applyTransform([]PathCommand{moveTo(0.0, 1.0)}, &tr)
	if out[0].Op != opMoveTo {
		t.Fatalf("expected MoveTo, got %v", out[0].Op)
	}
	if math.Abs(out[0].X-0.5) > 1e-5 {
		t.Fatalf("x should be 0.5, got %v", out[0].X)
	}
	if math.Abs(out[0].Y-1.0) > 1e-5 {
		t.Fatalf("y should be 1.0, got %v", out[0].Y)
	}
}

func TestStrokeWeightDeltaExpandsOuterAndShrinksInnerContours(t *testing.T) {
	input := []PathCommand{
		moveTo(0.0, 0.0),
		lineTo(100.0, 0.0),
		lineTo(100.0, 100.0),
		lineTo(0.0, 100.0),
		closePath(),
		moveTo(30.0, 30.0),
		lineTo(30.0, 70.0),
		lineTo(70.0, 70.0),
		lineTo(70.0, 30.0),
		closePath(),
	}

	output := applyStrokeWeightDelta(input, 10.0)
	contours := splitContours(output)
	if len(contours) != 2 {
		t.Fatalf("expected 2 contours, got %d", len(contours))
	}

	outer := onCurvePoints(contours[0])
	inner := onCurvePoints(contours[1])
	outerMinX, outerMinY, outerMaxX, outerMaxY := pointBounds(outer)
	innerMinX, innerMinY, innerMaxX, innerMaxY := pointBounds(inner)

	if outerMinX >= 0.0 || outerMinY >= 0.0 || outerMaxX <= 100.0 || outerMaxY <= 100.0 {
		t.Fatalf("outer contour should expand: %v %v %v %v", outerMinX, outerMinY, outerMaxX, outerMaxY)
	}
	if innerMinX <= 30.0 || innerMinY <= 30.0 || innerMaxX >= 70.0 || innerMaxY >= 70.0 {
		t.Fatalf("inner contour should shrink: %v %v %v %v", innerMinX, innerMinY, innerMaxX, innerMaxY)
	}
}

func TestStrokeWeightDeltaExpandsMultipleOuterIslandsWithMixedWinding(t *testing.T) {
	input := []PathCommand{
		moveTo(0.0, 0.0),
		lineTo(100.0, 0.0),
		lineTo(100.0, 100.0),
		lineTo(0.0, 100.0),
		closePath(),
		moveTo(240.0, 0.0),
		lineTo(240.0, 100.0),
		lineTo(140.0, 100.0),
		lineTo(140.0, 0.0),
		closePath(),
	}

	output := applyStrokeWeightDelta(input, 10.0)
	contours := splitContours(output)
	if len(contours) != 2 {
		t.Fatalf("expected 2 contours, got %d", len(contours))
	}

	first := onCurvePoints(contours[0])
	second := onCurvePoints(contours[1])
	firstMinX, _, firstMaxX, _ := pointBounds(first)
	secondMinX, _, secondMaxX, _ := pointBounds(second)

	if firstMinX >= 0.0 || firstMaxX <= 100.0 {
		t.Fatalf("first contour should expand: %v %v", firstMinX, firstMaxX)
	}
	if secondMinX >= 140.0 || secondMaxX <= 240.0 {
		t.Fatalf("second contour should expand: %v %v", secondMinX, secondMaxX)
	}
}

func TestStrokeWeightDeltaHandlesQuadraticContours(t *testing.T) {
	input := []PathCommand{
		moveTo(50.0, 0.0),
		quadTo(100.0, 0.0, 100.0, 50.0),
		quadTo(100.0, 100.0, 50.0, 100.0),
		quadTo(0.0, 100.0, 0.0, 50.0),
		quadTo(0.0, 0.0, 50.0, 0.0),
		closePath(),
	}

	output := applyStrokeWeightDelta(input, 8.0)
	contours := splitContours(output)
	points := onCurvePoints(contours[len(contours)-1])
	minX, minY, maxX, maxY := pointBounds(points)

	if minX >= 0.0 || minY >= 0.0 || maxX <= 100.0 || maxY <= 100.0 {
		t.Fatalf("quadratic contour should expand: %v %v %v %v", minX, minY, maxX, maxY)
	}
}

func TestStrokeWeightDeltaHandlesCubicContours(t *testing.T) {
	input := []PathCommand{
		moveTo(50.0, 0.0),
		curveTo(85.0, 0.0, 100.0, 15.0, 100.0, 50.0),
		curveTo(100.0, 85.0, 85.0, 100.0, 50.0, 100.0),
		curveTo(15.0, 100.0, 0.0, 85.0, 0.0, 50.0),
		curveTo(0.0, 15.0, 15.0, 0.0, 50.0, 0.0),
		closePath(),
	}

	output := applyStrokeWeightDelta(input, 8.0)
	contours := splitContours(output)
	points := onCurvePoints(contours[len(contours)-1])
	minX, minY, maxX, maxY := pointBounds(points)

	if minX >= 0.0 || minY >= 0.0 || maxX <= 100.0 || maxY <= 100.0 {
		t.Fatalf("cubic contour should expand: %v %v %v %v", minX, minY, maxX, maxY)
	}
}

func TestComputeCenter(t *testing.T) {
	cx, cy := computeCenter([]PathCommand{
		moveTo(0.0, 0.0),
		lineTo(100.0, 50.0),
		closePath(),
	})
	if cx != 50.0 || cy != 25.0 {
		t.Fatalf("expected center (50, 25), got (%v, %v)", cx, cy)
	}
}

func commandsEqual(a, b [][]PathCommand) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
