package main

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func TestWriteSFNTChecksums(t *testing.T) {
	head := make([]byte, 54)
	putU32(head, 0, 0x00010000)
	putU32(head, 12, 0x5F0F3CF5)
	putU16(head, 18, 1000)

	tables := []outTable{
		{Tag: "head", Data: head},
		{Tag: "maxp", Data: buildMaxpV1(1, 0, 0)},
		{Tag: "test", Data: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01}},
	}

	data, err := writeSFNT(tables)
	if err != nil {
		t.Fatalf("writeSFNT failed: %v", err)
	}

	// The whole-font checksum must equal the OpenType magic constant.
	if got := checksum(data); got != 0xB1B0AFBA {
		t.Fatalf("whole-font checksum = %#x, want 0xB1B0AFBA", got)
	}

	numTables := int(beU16(data, 4))
	for i := 0; i < numTables; i++ {
		rec := 12 + 16*i
		tag := string(data[rec : rec+4])
		want := beU32(data, rec+4)
		offset := int(beU32(data, rec+8))
		length := int(beU32(data, rec+12))

		table := make([]byte, length)
		copy(table, data[offset:offset+length])
		if tag == "head" {
			// Table checksums for head are computed with
			// checkSumAdjustment treated as zero.
			putU32(table, 8, 0)
		}
		if got := checksum(table); got != want {
			t.Errorf("table %q checksum = %#x, want %#x", tag, got, want)
		}
	}

	parsed, err := parseSFNT(data)
	if err != nil {
		t.Fatalf("parseSFNT failed: %v", err)
	}
	if len(parsed.tables) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(parsed.tables))
	}
	if got := beU32(mustTable(t, parsed, "head"), 8); got == 0 {
		t.Fatal("head.checkSumAdjustment should be non-zero")
	}
}

func TestWriteSFNTRejectsDuplicateTags(t *testing.T) {
	head := make([]byte, 54)
	if _, err := writeSFNT([]outTable{
		{Tag: "head", Data: head},
		{Tag: "head", Data: head},
	}); err == nil {
		t.Fatal("expected duplicate table tags to be rejected")
	}
}

func TestParseSFNTReadsFirstTTCFace(t *testing.T) {
	// Wrap the Go Regular font in a minimal two-entry TTC header. Both entries
	// point at the same face, which is enough to exercise offset handling.
	// Table offsets in a collection are absolute from the start of the file, so
	// shift the inner directory by the TTC header size.
	inner := append([]byte(nil), goregular.TTF...)
	const ttcHeaderSize = 20
	numTables := int(beU16(inner, 4))
	for i := 0; i < numTables; i++ {
		rec := 12 + 16*i
		putU32(inner, rec+8, beU32(inner, rec+8)+ttcHeaderSize)
	}

	header := make([]byte, ttcHeaderSize)
	copy(header[0:4], "ttcf")
	putU32(header, 4, 0x00010000)
	putU32(header, 8, 2)
	putU32(header, 12, ttcHeaderSize)
	putU32(header, 16, ttcHeaderSize)

	data := append(header, inner...)
	f, err := parseSFNT(data)
	if err != nil {
		t.Fatalf("parseSFNT failed: %v", err)
	}
	if _, ok := f.table("head"); !ok {
		t.Fatal("expected head table from the first TTC face")
	}

	// Ensure the TTC wrapper did not corrupt the table bytes.
	if beU32(mustTable(t, f, "maxp"), 0) != 0x00010000 {
		t.Fatal("maxp version mismatch")
	}
}
