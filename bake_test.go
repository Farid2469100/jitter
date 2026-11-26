package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

// gsubCounts returns the ScriptList, FeatureList and LookupList record counts.
func gsubCounts(t *testing.T, data []byte) (scripts, features, lookups int) {
	t.Helper()
	if len(data) < 10 {
		t.Fatalf("GSUB table is truncated")
	}
	scriptOff := int(beU16(data, 4))
	featureOff := int(beU16(data, 6))
	lookupOff := int(beU16(data, 8))
	if scriptOff+2 > len(data) || featureOff+2 > len(data) || lookupOff+2 > len(data) {
		t.Fatalf("GSUB offsets are out of bounds")
	}
	return int(beU16(data, scriptOff)), int(beU16(data, featureOff)), int(beU16(data, lookupOff))
}

func TestBuildGSUBHandlesEmpty(t *testing.T) {
	gsub, err := buildGSUB([][]uint16{{}, {}})
	if err != nil {
		t.Fatalf("buildGSUB failed: %v", err)
	}
	scripts, features, lookups := gsubCounts(t, gsub)
	if scripts != 1 {
		t.Errorf("expected 1 script (DFLT), got %d", scripts)
	}
	if features != 0 {
		t.Errorf("expected 0 features, got %d", features)
	}
	if lookups != 0 {
		t.Errorf("expected 0 lookups, got %d", lookups)
	}
}

func TestBuildGSUBUsesSharedStageLookups(t *testing.T) {
	altMap := make([][]uint16, 5)
	altMap[1] = []uint16{3, 4}
	gsub, err := buildGSUB(altMap)
	if err != nil {
		t.Fatalf("buildGSUB failed: %v", err)
	}
	scripts, features, lookups := gsubCounts(t, gsub)
	if scripts != 1 || features != 1 {
		t.Fatalf("expected 1 script and 1 feature, got %d and %d", scripts, features)
	}
	// 2 SingleSubst + 1 ChainContextSubst.
	if lookups != 3 {
		t.Fatalf("expected 3 lookups, got %d", lookups)
	}
}

func TestBuildGSUBReusesStageLookupsAcrossOrigins(t *testing.T) {
	altMap := make([][]uint16, 8)
	altMap[1] = []uint16{3, 4}
	altMap[2] = []uint16{5, 6}
	gsub, err := buildGSUB(altMap)
	if err != nil {
		t.Fatalf("buildGSUB failed: %v", err)
	}
	_, _, lookups := gsubCounts(t, gsub)
	if lookups != 3 {
		t.Fatalf("expected 3 lookups (2 stages + 1 chain), got %d", lookups)
	}
}

func TestBuildGSUBChunksLargeAltMaps(t *testing.T) {
	const n = chainChunkSize + 10
	altMap := make([][]uint16, n)
	for i := 1; i < n; i++ {
		altMap[i] = []uint16{uint16(n + i)}
	}
	gsub, err := buildGSUB(altMap)
	if err != nil {
		t.Fatalf("buildGSUB failed: %v", err)
	}
	_, _, lookups := gsubCounts(t, gsub)
	// 1 stage + 2 chain-context chunks.
	if lookups != 3 {
		t.Fatalf("expected 3 lookups, got %d", lookups)
	}
}

func TestBuildSimpleGlyphWithCubicProducesContours(t *testing.T) {
	commands := []PathCommand{
		moveTo(0.0, 0.0),
		curveTo(10.0, 0.0, 20.0, 10.0, 30.0, 10.0),
		closePath(),
	}
	data, info, err := buildSimpleGlyph(commands)
	if err != nil {
		t.Fatalf("buildSimpleGlyph failed: %v", err)
	}
	if info.empty || len(data) == 0 {
		t.Fatal("cubic commands should produce a non-empty simple glyph")
	}
	if info.numContours != 1 {
		t.Fatalf("expected 1 contour, got %d", info.numContours)
	}
	if info.numPoints < 3 {
		t.Fatalf("expected at least 3 points after cubic approximation, got %d", info.numPoints)
	}
}

func TestBuildSimpleGlyphEmpty(t *testing.T) {
	data, info, err := buildSimpleGlyph(nil)
	if err != nil {
		t.Fatalf("buildSimpleGlyph failed: %v", err)
	}
	if !info.empty || len(data) != 0 {
		t.Fatalf("empty input should produce an empty glyph, got %d bytes", len(data))
	}
}

func TestBuildSimpleGlyphOpenContourIsClosed(t *testing.T) {
	// go-text outlines are closed by the loader; a bare MoveTo/LineTo without
	// Close must still become a contour so bake never silently drops glyphs.
	commands := []PathCommand{
		moveTo(0.0, 0.0),
		lineTo(100.0, 0.0),
		lineTo(100.0, 100.0),
	}
	_, info, err := buildSimpleGlyph(commands)
	if err != nil {
		t.Fatalf("buildSimpleGlyph failed: %v", err)
	}
	if info.empty || info.numContours != 1 {
		t.Fatalf("expected one contour, got %+v", info)
	}
}

func TestDefaultBakeOutputPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{filepath.Join("fonts", "demo.otf"), filepath.Join("fonts", "demo-jittered.ttf")},
		{filepath.Join("fonts", "demo.ttf"), filepath.Join("fonts", "demo-jittered.ttf")},
		{"demo", "demo-jittered.ttf"},
		{".ttf", ".ttf-jittered.ttf"},
	}
	for _, c := range cases {
		if got := defaultBakeOutputPath(c.in); got != c.want {
			t.Errorf("defaultBakeOutputPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBakeGoRegular(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "goregular.ttf")
	if err := os.WriteFile(inputPath, goregular.TTF, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	outputPath := filepath.Join(dir, "goregular-jittered.ttf")

	const alternates = 3
	if err := bakeFont(inputPath, outputPath, alternates, 0.5); err != nil {
		t.Fatalf("bakeFont failed: %v", err)
	}

	baked, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read baked font: %v", err)
	}

	origSFNT, err := parseSFNT(goregular.TTF)
	if err != nil {
		t.Fatalf("parse original: %v", err)
	}
	origMaxp, _ := parseMaxp(mustTable(t, origSFNT, "maxp"))
	origHead, _ := parseHead(mustTable(t, origSFNT, "head"))
	origHhea, _ := parseHhea(mustTable(t, origSFNT, "hhea"))
	origMetrics, err := parseHmtx(mustTable(t, origSFNT, "hmtx"), int(origMaxp.numGlyphs), int(origHhea.numberOfHMetrics))
	if err != nil {
		t.Fatalf("parse original hmtx: %v", err)
	}

	origFaces, err := font.ParseTTC(bytes.NewReader(goregular.TTF))
	if err != nil {
		t.Fatalf("parse original with go-text: %v", err)
	}
	origFace := origFaces[0]
	nonEmpty := 0
	for gid := 1; gid < int(origMaxp.numGlyphs); gid++ {
		outline, _ := origFace.GlyphDataOutline(font.GID(gid))
		if len(outline.Segments) > 0 {
			nonEmpty++
		}
	}
	wantGlyphs := int(origMaxp.numGlyphs) + nonEmpty*alternates

	bakedSFNT, err := parseSFNT(baked)
	if err != nil {
		t.Fatalf("parse baked: %v", err)
	}
	bakedMaxp, err := parseMaxp(mustTable(t, bakedSFNT, "maxp"))
	if err != nil {
		t.Fatalf("parse baked maxp: %v", err)
	}
	if int(bakedMaxp.numGlyphs) != wantGlyphs {
		t.Fatalf("numGlyphs = %d, want %d", bakedMaxp.numGlyphs, wantGlyphs)
	}
	if int(bakedMaxp.numGlyphs) <= int(origMaxp.numGlyphs) {
		t.Fatal("baking should add alternate glyphs")
	}

	bakedHead, err := parseHead(mustTable(t, bakedSFNT, "head"))
	if err != nil {
		t.Fatalf("parse baked head: %v", err)
	}
	if bakedHead.unitsPerEm != origHead.unitsPerEm {
		t.Fatalf("unitsPerEm changed: %d -> %d", origHead.unitsPerEm, bakedHead.unitsPerEm)
	}
	if beU32(mustTable(t, bakedSFNT, "post"), 0) != 0x00030000 {
		t.Fatal("post table should be format 3.0")
	}
	if _, ok := bakedSFNT.table("CFF "); ok {
		t.Fatal("baked TTF should not contain a CFF table")
	}

	bakedHhea, err := parseHhea(mustTable(t, bakedSFNT, "hhea"))
	if err != nil {
		t.Fatalf("parse baked hhea: %v", err)
	}
	if int(bakedHhea.numberOfHMetrics) != wantGlyphs {
		t.Fatalf("numberOfHMetrics = %d, want %d", bakedHhea.numberOfHMetrics, wantGlyphs)
	}
	bakedMetrics, err := parseHmtx(mustTable(t, bakedSFNT, "hmtx"), wantGlyphs, int(bakedHhea.numberOfHMetrics))
	if err != nil {
		t.Fatalf("parse baked hmtx: %v", err)
	}

	// Original glyph metrics must be preserved exactly; alternates inherit the
	// advance width and side bearing of their origin.
	for gid := 0; gid < int(origMaxp.numGlyphs); gid++ {
		if bakedMetrics[gid] != origMetrics[gid] {
			t.Fatalf("glyph %d metrics changed: %+v -> %+v", gid, origMetrics[gid], bakedMetrics[gid])
		}
	}
	altGid := int(origMaxp.numGlyphs)
	if bakedMetrics[altGid] != bakedMetrics[0] {
		// gid 0 (.notdef) gets no alternates, so the first alternate belongs to
		// the first non-empty glyph after it. Find that glyph and compare.
		origin := 1
		for {
			outline, _ := origFace.GlyphDataOutline(font.GID(origin))
			if len(outline.Segments) > 0 {
				break
			}
			origin++
		}
		if bakedMetrics[altGid] != bakedMetrics[origin] {
			t.Fatalf("alternate metrics %+v do not match origin %d metrics %+v", bakedMetrics[altGid], origin, bakedMetrics[origin])
		}
	}

	scripts, features, lookups := gsubCounts(t, mustTable(t, bakedSFNT, "GSUB"))
	if scripts != 1 || features != 1 {
		t.Fatalf("expected 1 script and 1 feature, got %d and %d", scripts, features)
	}
	if lookups < 2 {
		t.Fatalf("expected at least 2 lookups, got %d", lookups)
	}

	// HarfBuzz must be able to shape the baked font, and `calt` must change the
	// glyph run for repeated characters.
	faces, err := font.ParseTTC(bytes.NewReader(baked))
	if err != nil {
		t.Fatalf("go-text cannot parse baked font: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 face, got %d", len(faces))
	}
	face := faces[0]

	shape := func(text []rune, calt bool) []font.GID {
		feature := shaping.FontFeature{Tag: ot.MustNewTag("calt"), Value: 1}
		if !calt {
			feature.Value = 0
		}
		out := (&shaping.HarfbuzzShaper{}).Shape(shaping.Input{
			Text:         text,
			RunEnd:       len(text),
			Face:         face,
			Size:         fixed.I(16),
			Script:       language.Latin,
			Language:     language.NewLanguage("EN"),
			FontFeatures: []shaping.FontFeature{feature},
		})
		gids := make([]font.GID, len(out.Glyphs))
		for i, g := range out.Glyphs {
			gids[i] = g.GlyphID
		}
		return gids
	}

	text := []rune("aaaa")
	off := shape(text, false)
	on := shape(text, true)
	if len(on) != len(off) {
		t.Fatalf("calt changed the glyph count: %d -> %d", len(off), len(on))
	}
	changed := false
	for i := range on {
		if on[i] != off[i] {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("calt did not substitute any glyphs: off=%v on=%v", off, on)
	}
}

func TestBakeRejectsInvalidInput(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.ttf")
	if err := os.WriteFile(bad, []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bakeFont(bad, filepath.Join(dir, "out.ttf"), 1, 0.5); err == nil {
		t.Fatal("expected bake to fail for a non-font file")
	}
	if err := bakeFont(bad, filepath.Join(dir, "out.ttf"), 0, 0.5); err == nil {
		t.Fatal("expected bake to fail for zero alternates")
	}
	if err := bakeFont(filepath.Join(dir, "missing.ttf"), filepath.Join(dir, "out.ttf"), 1, 0.5); err == nil {
		t.Fatal("expected bake to fail for a missing file")
	}
}

func mustTable(t *testing.T, f *sfntFont, tag string) []byte {
	t.Helper()
	data, err := f.requireTable(tag)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
