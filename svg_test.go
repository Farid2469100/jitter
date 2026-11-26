package main

import (
	"strings"
	"testing"
)

func TestRenderSVGEmptyInput(t *testing.T) {
	svg := renderSVG(nil, nil, 48, 1000)
	if !strings.Contains(svg, "<svg ") {
		t.Fatalf("expected an svg element, got %q", svg)
	}
	if !strings.Contains(svg, `viewBox="0 0 0 72"`) {
		t.Fatalf("unexpected viewBox: %q", svg)
	}
	if strings.Contains(svg, "<path") {
		t.Fatalf("empty input should not produce paths: %q", svg)
	}
	if !strings.HasSuffix(svg, "</svg>\n") {
		t.Fatalf("expected trailing </svg>, got %q", svg)
	}
}

func TestRenderSVGProducesOnePathPerGlyph(t *testing.T) {
	glyphs := []GlyphData{
		{AdvanceWidth: 500, Commands: []PathCommand{
			moveTo(0, 0), lineTo(500, 0), lineTo(500, 500), lineTo(0, 500), closePath(),
		}},
		{AdvanceWidth: 250}, // space: no outline, still advances
		{AdvanceWidth: 500, Commands: []PathCommand{
			moveTo(0, 0), lineTo(500, 0), lineTo(500, 500), lineTo(0, 500), closePath(),
		}},
	}
	cmds := [][]PathCommand{glyphs[0].Commands, nil, glyphs[2].Commands}

	svg := renderSVG(glyphs, cmds, 48, 1000)
	if got := strings.Count(svg, "<path "); got != 2 {
		t.Fatalf("expected 2 paths, got %d: %q", got, svg)
	}
	if !strings.Contains(svg, `id="g0"`) || !strings.Contains(svg, `id="g2"`) {
		t.Fatalf("expected path ids g0 and g2: %q", svg)
	}
	if strings.Contains(svg, `id="g1"`) {
		t.Fatalf("space glyph should not produce a path: %q", svg)
	}
}

func TestRenderSVGViewBoxUsesAdvanceWidths(t *testing.T) {
	glyphs := []GlyphData{{AdvanceWidth: 1000}, {AdvanceWidth: 500}}
	svg := renderSVG(glyphs, nil, 48, 1000)
	// total advance 1500 * 48/1000 = 72; height ceil(48*1.5) = 72.
	if !strings.Contains(svg, `viewBox="0 0 72 72"`) {
		t.Fatalf("unexpected viewBox: %q", svg)
	}
}

func TestCommandsToPathDFormat(t *testing.T) {
	d := commandsToPathD([]PathCommand{
		moveTo(0, 0),
		lineTo(100, 100),
		quadTo(50, 150, 100, 200),
		curveTo(10, 20, 30, 40, 50, 60),
		closePath(),
	}, 0.5, 10, 100)

	want := "M10.00 100.00 L60.00 50.00 Q35.00 25.00 60.00 0.00 C15.00 90.00 25.00 80.00 35.00 70.00 Z"
	if d != want {
		t.Fatalf("unexpected path:\n got: %q\nwant: %q", d, want)
	}
}
