package main

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
)

func beU16(data []byte, off int) uint16 {
	return binary.BigEndian.Uint16(data[off : off+2])
}

func beI16(data []byte, off int) int16 {
	return int16(binary.BigEndian.Uint16(data[off : off+2]))
}

func beU32(data []byte, off int) uint32 {
	return binary.BigEndian.Uint32(data[off : off+4])
}

func putU16(dst []byte, off int, v uint16) {
	binary.BigEndian.PutUint16(dst[off:off+2], v)
}

func putI16(dst []byte, off int, v int16) {
	binary.BigEndian.PutUint16(dst[off:off+2], uint16(v))
}

func putU32(dst []byte, off int, v uint32) {
	binary.BigEndian.PutUint32(dst[off:off+4], v)
}

// sfntFont is a minimal SFNT table directory reader. It supports both
// standalone fonts and TrueType/OpenType collections (the first face is used).
type sfntFont struct {
	data   []byte
	tables map[string][]byte
	tags   []string
}

func parseSFNT(data []byte) (*sfntFont, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("font file is too short")
	}

	fontOffset := 0
	if string(data[0:4]) == "ttcf" {
		numFonts := int(beU32(data, 8))
		if numFonts < 1 {
			return nil, fmt.Errorf("font collection contains no fonts")
		}
		if len(data) < 12+4*numFonts {
			return nil, fmt.Errorf("font collection header is truncated")
		}
		fontOffset = int(beU32(data, 12))
	}

	if fontOffset < 0 || fontOffset+12 > len(data) {
		return nil, fmt.Errorf("invalid font offset")
	}

	numTables := int(beU16(data, fontOffset+4))
	recordsStart := fontOffset + 12
	if recordsStart+16*numTables > len(data) {
		return nil, fmt.Errorf("font table directory is truncated")
	}

	out := &sfntFont{
		data:   data,
		tables: make(map[string][]byte, numTables),
		tags:   make([]string, 0, numTables),
	}

	for i := 0; i < numTables; i++ {
		rec := recordsStart + 16*i
		tag := string(data[rec : rec+4])
		offset := int(beU32(data, rec+8))
		length := int(beU32(data, rec+12))
		if offset < 0 || length < 0 || offset+length > len(data) {
			return nil, fmt.Errorf("table %q is out of bounds", tag)
		}
		if _, exists := out.tables[tag]; exists {
			continue
		}
		out.tables[tag] = data[offset : offset+length]
		out.tags = append(out.tags, tag)
	}

	return out, nil
}

func (f *sfntFont) table(tag string) ([]byte, bool) {
	data, ok := f.tables[tag]
	return data, ok
}

func (f *sfntFont) requireTable(tag string) ([]byte, error) {
	data, ok := f.tables[tag]
	if !ok {
		return nil, fmt.Errorf("font is missing %q table", tag)
	}
	return data, nil
}

type headInfo struct {
	unitsPerEm       uint16
	indexToLocFormat int16
	raw              []byte
}

func parseHead(data []byte) (headInfo, error) {
	if len(data) < 54 {
		return headInfo{}, fmt.Errorf("head table is truncated")
	}
	return headInfo{
		unitsPerEm:       beU16(data, 18),
		indexToLocFormat: beI16(data, 50),
		raw:              data,
	}, nil
}

type maxpInfo struct {
	version   uint32
	numGlyphs uint16
}

func parseMaxp(data []byte) (maxpInfo, error) {
	if len(data) < 6 {
		return maxpInfo{}, fmt.Errorf("maxp table is truncated")
	}
	return maxpInfo{
		version:   beU32(data, 0),
		numGlyphs: beU16(data, 4),
	}, nil
}

type hheaInfo struct {
	numberOfHMetrics uint16
	raw              []byte
}

func parseHhea(data []byte) (hheaInfo, error) {
	if len(data) < 36 {
		return hheaInfo{}, fmt.Errorf("hhea table is truncated")
	}
	return hheaInfo{
		numberOfHMetrics: beU16(data, 34),
		raw:              data,
	}, nil
}

// longMetric is one horizontal metric entry.
type longMetric struct {
	Advance uint16
	LSB     int16
}

func parseHmtx(data []byte, numGlyphs, numberOfHMetrics int) ([]longMetric, error) {
	if numGlyphs < 0 {
		return nil, fmt.Errorf("invalid glyph count")
	}
	if numberOfHMetrics < 0 {
		return nil, fmt.Errorf("invalid hhea metric count")
	}
	if numberOfHMetrics > numGlyphs {
		numberOfHMetrics = numGlyphs
	}

	needed := numberOfHMetrics * 4
	if len(data) < needed {
		return nil, fmt.Errorf("hmtx table is truncated")
	}

	metrics := make([]longMetric, numGlyphs)
	lastAdvance := uint16(0)

	for i := 0; i < numberOfHMetrics; i++ {
		metrics[i] = longMetric{
			Advance: beU16(data, i*4),
			LSB:     beI16(data, i*4+2),
		}
		lastAdvance = metrics[i].Advance
	}

	for i := numberOfHMetrics; i < numGlyphs; i++ {
		lsb := int16(0)
		lsbOff := needed + 2*(i-numberOfHMetrics)
		if lsbOff+2 <= len(data) {
			lsb = beI16(data, lsbOff)
		}
		metrics[i] = longMetric{Advance: lastAdvance, LSB: lsb}
	}

	return metrics, nil
}

// outTable is one table to write into an SFNT container.
type outTable struct {
	Tag  string
	Data []byte
}

// writeSFNT assembles tables into a TrueType font file with a correct table
// directory, checksums, 4-byte alignment and head.checkSumAdjustment.
func writeSFNT(tables []outTable) ([]byte, error) {
	if len(tables) == 0 {
		return nil, fmt.Errorf("cannot write a font with no tables")
	}

	sorted := make([]outTable, len(tables))
	copy(sorted, tables)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tag < sorted[j].Tag })

	for i, t := range sorted {
		if len(t.Tag) != 4 {
			return nil, fmt.Errorf("invalid table tag %q", t.Tag)
		}
		if i > 0 && sorted[i-1].Tag == t.Tag {
			return nil, fmt.Errorf("duplicate table tag %q", t.Tag)
		}
	}

	// The head table's checkSumAdjustment must be zero while computing
	// checksums. We work on our own copy so the caller's slice is untouched.
	for i := range sorted {
		if sorted[i].Tag == "head" {
			if len(sorted[i].Data) < 12 {
				return nil, fmt.Errorf("head table is truncated")
			}
			headCopy := make([]byte, len(sorted[i].Data))
			copy(headCopy, sorted[i].Data)
			putU32(headCopy, 8, 0)
			sorted[i].Data = headCopy
		}
	}

	numTables := len(sorted)
	directorySize := 12 + 16*numTables

	offsets := make([]int, numTables)
	cursor := directorySize
	for i, t := range sorted {
		cursor = align4(cursor)
		offsets[i] = cursor
		cursor += len(t.Data)
	}
	totalSize := align4(cursor)

	out := make([]byte, totalSize)

	putU32(out, 0, 0x00010000) // TrueType outlines
	putU16(out, 4, uint16(numTables))
	log2 := bits.Len(uint(numTables)) - 1
	searchRange := (1 << log2) * 16
	entrySelector := log2
	rangeShift := numTables*16 - searchRange
	putU16(out, 6, uint16(searchRange))
	putU16(out, 8, uint16(entrySelector))
	putU16(out, 10, uint16(rangeShift))

	for i, t := range sorted {
		rec := 12 + 16*i
		copy(out[rec:rec+4], t.Tag)
		putU32(out, rec+4, checksum(t.Data))
		putU32(out, rec+8, uint32(offsets[i]))
		putU32(out, rec+12, uint32(len(t.Data)))
		copy(out[offsets[i]:], t.Data)
	}

	// Locate head and compute the whole-font checksum.
	headOffset := -1
	for i, t := range sorted {
		if t.Tag == "head" {
			headOffset = offsets[i]
			break
		}
	}
	if headOffset >= 0 {
		sum := checksum(out)
		adjustment := uint32(0xB1B0AFBA) - sum
		putU32(out, headOffset+8, adjustment)
	}

	return out, nil
}

func align4(n int) int {
	return (n + 3) &^ 3
}

// checksum returns the OpenType checksum of data, treating the data as if it
// were padded with zero bytes to a multiple of four.
func checksum(data []byte) uint32 {
	var sum uint32
	i := 0
	for ; i+4 <= len(data); i += 4 {
		sum += binary.BigEndian.Uint32(data[i : i+4])
	}
	if rem := len(data) - i; rem > 0 {
		var tail [4]byte
		copy(tail[:], data[i:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}

func buildLoca(offsets []uint32, short bool) []byte {
	if short {
		out := make([]byte, 2*len(offsets))
		for i, off := range offsets {
			putU16(out, i*2, uint16(off/2))
		}
		return out
	}

	out := make([]byte, 4*len(offsets))
	for i, off := range offsets {
		putU32(out, i*4, off)
	}
	return out
}

func buildHead(orig []byte, unitsPerEm uint16, indexToLocFormat int16, bbox [4]int16) []byte {
	out := make([]byte, 54)
	if len(orig) >= 54 {
		copy(out, orig[:54])
	} else {
		putU32(out, 0, 0x00010000)  // version
		putU32(out, 12, 0x5F0F3CF5) // magic
		putI16(out, 48, 2)          // fontDirectionHint
		putU16(out, 46, 8)          // lowestRecPPEM
	}

	putU32(out, 0, 0x00010000)
	putU32(out, 8, 0) // checkSumAdjustment; fixed up by writeSFNT
	putU32(out, 12, 0x5F0F3CF5)
	putU16(out, 18, unitsPerEm)
	putI16(out, 36, bbox[0])
	putI16(out, 38, bbox[1])
	putI16(out, 40, bbox[2])
	putI16(out, 42, bbox[3])
	putI16(out, 50, indexToLocFormat)
	putI16(out, 52, 0) // glyphDataFormat

	return out
}

func buildMaxpV1(numGlyphs, maxPoints, maxContours uint16) []byte {
	out := make([]byte, 32)
	putU32(out, 0, 0x00010000)
	putU16(out, 4, numGlyphs)
	putU16(out, 6, maxPoints)
	putU16(out, 8, maxContours)
	putU16(out, 10, 0) // maxCompositePoints
	putU16(out, 12, 0) // maxCompositeContours
	putU16(out, 14, 2) // maxZones
	putU16(out, 16, 0) // maxTwilightPoints
	putU16(out, 18, 0) // maxStorage
	putU16(out, 20, 0) // maxFunctionDefs
	putU16(out, 22, 0) // maxInstructionDefs
	putU16(out, 24, 0) // maxStackElements
	putU16(out, 26, 0) // maxSizeOfInstructions
	putU16(out, 28, 0) // maxComponentElements
	putU16(out, 30, 0) // maxComponentDepth
	return out
}

func buildHhea(orig []byte, metrics []longMetric, infos []glyphInfo) []byte {
	out := make([]byte, 36)
	if len(orig) >= 36 {
		copy(out, orig[:36])
	} else {
		putU32(out, 0, 0x00010000)
	}

	advanceWidthMax := uint16(0)
	minLSB := int64(1 << 30)
	minRSB := int64(1 << 30)
	maxExtent := int64(-(1 << 30))

	for i, metric := range metrics {
		if metric.Advance > advanceWidthMax {
			advanceWidthMax = metric.Advance
		}

		lsb := int64(metric.LSB)
		width := int64(0)
		if i < len(infos) && !infos[i].empty {
			width = int64(infos[i].xMax) - int64(infos[i].xMin)
		}

		if lsb < minLSB {
			minLSB = lsb
		}
		rsb := int64(metric.Advance) - lsb - width
		if rsb < minRSB {
			minRSB = rsb
		}
		extent := lsb + width
		if extent > maxExtent {
			maxExtent = extent
		}
	}

	if len(metrics) == 0 {
		minLSB = 0
		minRSB = 0
		maxExtent = 0
	}

	putU32(out, 0, 0x00010000)
	putU16(out, 10, advanceWidthMax)
	putI16(out, 12, clampI16(minLSB))
	putI16(out, 14, clampI16(minRSB))
	putI16(out, 16, clampI16(maxExtent))
	putI16(out, 32, 0) // metricDataFormat
	putU16(out, 34, uint16(len(metrics)))
	return out
}

func buildHmtx(metrics []longMetric) []byte {
	out := make([]byte, 4*len(metrics))
	for i, metric := range metrics {
		putU16(out, i*4, metric.Advance)
		putI16(out, i*4+2, metric.LSB)
	}
	return out
}

// buildPostV3 creates a format 3.0 post table. Format 3 stores no glyph names,
// so it stays consistent when the glyph count changes.
func buildPostV3(orig []byte) []byte {
	out := make([]byte, 32)
	if len(orig) >= 32 {
		copy(out, orig[:32])
	}
	putU32(out, 0, 0x00030000)
	return out
}

func clampI16(v int64) int16 {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return int16(v)
}

func clampU16(v int64) uint16 {
	if v < 0 {
		return 0
	}
	if v > 65535 {
		return 65535
	}
	return uint16(v)
}
