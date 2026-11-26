package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/go-text/typesetting/font"
)

// Tables that are rebuilt from scratch.
var replacedTables = map[string]bool{
	"head": true,
	"maxp": true,
	"hhea": true,
	"hmtx": true,
	"glyf": true,
	"loca": true,
	"post": true,
	"GSUB": true,
}

// Tables that cannot be carried over safely after changing the glyph count or
// outline format. Most of them are indexed per glyph, are tied to CFF
// outlines, or describe a variable font that no longer exists.
var droppedTables = map[string]bool{
	"CFF ": true,
	"CFF2": true,
	"VORG": true,
	"DSIG": true,
	"hdmx": true,
	"LTSH": true,
	"VDMX": true,
	"vhea": true,
	"vmtx": true,
	"fvar": true,
	"gvar": true,
	"avar": true,
	"HVAR": true,
	"VVAR": true,
	"MVAR": true,
	"cvar": true,
	"STAT": true,
	"JSTF": true,
	"sbix": true,
	"CBDT": true,
	"CBLC": true,
	"EBDT": true,
	"EBLC": true,
	"EBSC": true,
}

// bakeFont reads the font at inputPath, generates alternates jittered variants
// per glyph, and writes a new TTF with the calt GSUB feature to outputPath.
func bakeFont(inputPath, outputPath string, alternates uint32, intensity float64) error {
	if alternates == 0 {
		return fmt.Errorf("alternates must be at least 1")
	}
	if alternates > 65535 {
		return fmt.Errorf("alternates must be at most 65535")
	}

	fontData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read font file: %w", err)
	}

	sfnt, err := parseSFNT(fontData)
	if err != nil {
		return fmt.Errorf("failed to parse font: %w", err)
	}

	headData, err := sfnt.requireTable("head")
	if err != nil {
		return err
	}
	head, err := parseHead(headData)
	if err != nil {
		return fmt.Errorf("failed to read head: %w", err)
	}
	if head.unitsPerEm == 0 {
		return fmt.Errorf("font has unitsPerEm = 0")
	}

	maxpData, err := sfnt.requireTable("maxp")
	if err != nil {
		return err
	}
	maxp, err := parseMaxp(maxpData)
	if err != nil {
		return fmt.Errorf("failed to read maxp: %w", err)
	}
	numGlyphs := int(maxp.numGlyphs)
	if numGlyphs == 0 {
		return fmt.Errorf("font contains no glyphs")
	}

	hheaData, err := sfnt.requireTable("hhea")
	if err != nil {
		return err
	}
	hhea, err := parseHhea(hheaData)
	if err != nil {
		return fmt.Errorf("failed to read hhea: %w", err)
	}

	hmtxData, err := sfnt.requireTable("hmtx")
	if err != nil {
		return err
	}
	originalMetrics, err := parseHmtx(hmtxData, numGlyphs, int(hhea.numberOfHMetrics))
	if err != nil {
		return fmt.Errorf("failed to read hmtx: %w", err)
	}

	// Parse through go-text for outline extraction. Collections are rejected
	// for bake mode, because the output is a single font.
	faces, err := font.ParseTTC(bytes.NewReader(fontData))
	if err != nil {
		return fmt.Errorf("failed to parse font: %w", err)
	}
	if len(faces) == 0 {
		return fmt.Errorf("font contains no faces")
	}
	if len(faces) > 1 {
		return fmt.Errorf("font collections (.ttc/.otc) are not yet supported")
	}
	face := faces[0]

	originals := make([][]PathCommand, numGlyphs)
	for gid := 0; gid < numGlyphs; gid++ {
		outline, _ := face.GlyphDataOutline(font.GID(gid))
		originals[gid] = segmentsToCommands(outline.Segments)
	}

	// Originals first, then alternates. Original glyph IDs are preserved.
	newGlyphs := make([][]PathCommand, 0, numGlyphs)
	newMetrics := make([]longMetric, 0, numGlyphs)
	altMap := make([][]uint16, numGlyphs)

	for gid := 0; gid < numGlyphs; gid++ {
		newGlyphs = append(newGlyphs, originals[gid])
		newMetrics = append(newMetrics, originalMetrics[gid])
	}

	upem := float64(head.unitsPerEm)

	for gid := 1; gid < numGlyphs; gid++ {
		if len(originals[gid]) == 0 {
			continue
		}
		for a := uint32(0); a < alternates; a++ {
			if len(newGlyphs) >= 65535 {
				return fmt.Errorf("too many glyphs after baking: exceeds %d", 65535)
			}

			jittered := applyJitterOne(originals[gid], intensity, upem)
			newGlyphs = append(newGlyphs, jittered)
			// The alternate inherits the advance width and side bearing of
			// its origin.
			newMetrics = append(newMetrics, originalMetrics[gid])
			altMap[gid] = append(altMap[gid], uint16(len(newGlyphs)-1))
		}
	}

	totalGlyphs := uint16(len(newGlyphs))

	glyfData, offsets, infos, err := buildGlyf(newGlyphs)
	if err != nil {
		return fmt.Errorf("failed to build glyf: %w", err)
	}

	shortLoca := true
	for _, off := range offsets {
		if off > 0x1FFFE || off%2 != 0 {
			shortLoca = false
			break
		}
	}
	locaData := buildLoca(offsets, shortLoca)

	indexToLocFormat := int16(1)
	if shortLoca {
		indexToLocFormat = 0
	}

	maxPoints := uint16(0)
	maxContours := uint16(0)
	var fontBBox [4]int16
	haveBBox := false

	for _, info := range infos {
		if info.numPoints > maxPoints {
			maxPoints = info.numPoints
		}
		if info.numContours > maxContours {
			maxContours = info.numContours
		}
		if info.empty {
			continue
		}
		if !haveBBox {
			fontBBox = [4]int16{info.xMin, info.yMin, info.xMax, info.yMax}
			haveBBox = true
		} else {
			if info.xMin < fontBBox[0] {
				fontBBox[0] = info.xMin
			}
			if info.yMin < fontBBox[1] {
				fontBBox[1] = info.yMin
			}
			if info.xMax > fontBBox[2] {
				fontBBox[2] = info.xMax
			}
			if info.yMax > fontBBox[3] {
				fontBBox[3] = info.yMax
			}
		}
	}

	newHead := buildHead(head.raw, head.unitsPerEm, indexToLocFormat, fontBBox)
	newMaxp := buildMaxpV1(totalGlyphs, maxPoints, maxContours)
	newHhea := buildHhea(hhea.raw, newMetrics, infos)
	newHmtx := buildHmtx(newMetrics)

	postData, _ := sfnt.table("post")
	newPost := buildPostV3(postData)

	gsubData, err := buildGSUB(altMap)
	if err != nil {
		return fmt.Errorf("failed to build GSUB: %w", err)
	}

	tables := []outTable{
		{Tag: "GSUB", Data: gsubData},
		{Tag: "glyf", Data: glyfData},
		{Tag: "head", Data: newHead},
		{Tag: "hhea", Data: newHhea},
		{Tag: "hmtx", Data: newHmtx},
		{Tag: "loca", Data: locaData},
		{Tag: "maxp", Data: newMaxp},
		{Tag: "post", Data: newPost},
	}

	for tag, data := range sfnt.tables {
		if replacedTables[tag] || droppedTables[tag] {
			continue
		}
		tables = append(tables, outTable{Tag: tag, Data: data})
	}

	out, err := writeSFNT(tables)
	if err != nil {
		return fmt.Errorf("failed to write font: %w", err)
	}

	if err := verifyBakedFont(out, totalGlyphs); err != nil {
		return err
	}

	if err := os.WriteFile(outputPath, out, 0o644); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}

// verifyBakedFont re-parses the freshly baked font and checks that the glyph
// count and horizontal metrics are internally consistent.
func verifyBakedFont(data []byte, expectedNumGlyphs uint16) error {
	sfnt, err := parseSFNT(data)
	if err != nil {
		return fmt.Errorf("bake produced a font that failed re-parse: %w", err)
	}

	maxpData, err := sfnt.requireTable("maxp")
	if err != nil {
		return fmt.Errorf("bake produced a font without maxp: %w", err)
	}
	maxp, err := parseMaxp(maxpData)
	if err != nil {
		return fmt.Errorf("bake produced font whose maxp is unreadable: %w", err)
	}
	if maxp.numGlyphs != expectedNumGlyphs {
		return fmt.Errorf("bake produced inconsistent font: maxp.numGlyphs=%d, expected %d", maxp.numGlyphs, expectedNumGlyphs)
	}

	hheaData, err := sfnt.requireTable("hhea")
	if err != nil {
		return fmt.Errorf("bake produced a font without hhea: %w", err)
	}
	hhea, err := parseHhea(hheaData)
	if err != nil {
		return fmt.Errorf("bake produced font whose hhea is unreadable: %w", err)
	}
	if hhea.numberOfHMetrics != expectedNumGlyphs {
		return fmt.Errorf("bake produced inconsistent font: hhea.numberOfHMetrics=%d, expected %d", hhea.numberOfHMetrics, expectedNumGlyphs)
	}

	hmtxData, err := sfnt.requireTable("hmtx")
	if err != nil {
		return fmt.Errorf("bake produced a font without hmtx: %w", err)
	}
	metrics, err := parseHmtx(hmtxData, int(expectedNumGlyphs), int(hhea.numberOfHMetrics))
	if err != nil {
		return fmt.Errorf("bake produced font whose hmtx is unreadable: %w", err)
	}
	if len(metrics) != int(expectedNumGlyphs) {
		return fmt.Errorf("bake produced inconsistent font: hmtx metrics=%d, expected %d", len(metrics), expectedNumGlyphs)
	}

	for _, tag := range []string{"glyf", "loca", "head", "post", "GSUB"} {
		if _, ok := sfnt.table(tag); !ok {
			return fmt.Errorf("bake produced a font without %q table", tag)
		}
	}

	faces, err := font.ParseTTC(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("bake produced a font that go-text cannot parse: %w", err)
	}
	if len(faces) != 1 {
		return fmt.Errorf("bake produced %d faces, expected 1", len(faces))
	}

	return nil
}
