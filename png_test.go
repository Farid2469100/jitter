package main

import (
	"bytes"
	"image/png"
	"testing"
)

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func TestRenderPNGEmptyInput(t *testing.T) {
	data, err := renderPNG(nil, nil, 48, 1000)
	if err != nil {
		t.Fatalf("empty input should render: %v", err)
	}
	if len(data) < 8 || !bytes.Equal(data[:8], pngSignature) {
		t.Fatalf("missing PNG signature: % x", data[:min(len(data), 8)])
	}
}

func TestRenderPNGSignature(t *testing.T) {
	glyphs := []GlyphData{{AdvanceWidth: 500}}
	cmds := [][]PathCommand{{
		moveTo(0, 0), lineTo(500, 0), lineTo(500, 500), lineTo(0, 500), closePath(),
	}}
	data, err := renderPNG(glyphs, cmds, 48, 1000)
	if err != nil {
		t.Fatalf("dummy input should render: %v", err)
	}
	if !bytes.Equal(data[:8], pngSignature) {
		t.Fatalf("missing PNG signature: % x", data[:8])
	}
}

func TestRenderPNGRendersOpaquePixels(t *testing.T) {
	glyphs := []GlyphData{{AdvanceWidth: 500}}
	cmds := [][]PathCommand{{
		moveTo(0, 0), lineTo(500, 0), lineTo(500, 500), lineTo(0, 500), closePath(),
	}}
	img, err := buildImage(glyphs, cmds, 48, 1000)
	if err != nil {
		t.Fatalf("dummy input should render: %v", err)
	}

	hasOpaque := false
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			hasOpaque = true
			break
		}
	}
	if !hasOpaque {
		t.Fatal("expected at least one non-transparent pixel")
	}
}

func TestRenderPNGDecodes(t *testing.T) {
	glyphs := []GlyphData{{AdvanceWidth: 500}}
	cmds := [][]PathCommand{{
		moveTo(0, 0), lineTo(500, 0), lineTo(500, 500), lineTo(0, 500), closePath(),
	}}
	data, err := renderPNG(glyphs, cmds, 48, 1000)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if img.Bounds().Dx() != 24 || img.Bounds().Dy() != 72 {
		t.Fatalf("unexpected size: %v", img.Bounds())
	}
}
