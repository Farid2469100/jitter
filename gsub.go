package main

import (
	"fmt"
	"sort"
)

// gsubPair associates an original glyph with the alternate glyphs generated
// for it.
type gsubPair struct {
	origin uint16
	alts   []uint16
}

// gsubLookup is an OpenType extension lookup (type 7) wrapping a real lookup
// subtable. Extension lookups keep the LookupList offsets small even for large
// fonts, because the actual subtables are referenced with 32-bit offsets.
type gsubLookup struct {
	extensionType uint16
	subtable      []byte
}

type classEntry struct {
	gid   uint16
	class uint16
}

type gsubRule struct {
	backtrack uint16
	lookup    uint16
}

const chainChunkSize = 192

// buildGSUB builds a minimal GSUB table exposing `calt` backed by extension
// lookups: one SingleSubst per alternate stage and one ChainContextSubst per
// chunk of original glyphs. Consecutive identical glyphs cycle through their
// alternates.
func buildGSUB(altMap [][]uint16) ([]byte, error) {
	pairs := make([]gsubPair, 0, len(altMap))
	for gid, alts := range altMap {
		if len(alts) == 0 {
			continue
		}
		pairs = append(pairs, gsubPair{origin: uint16(gid), alts: alts})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].origin < pairs[j].origin })

	if len(pairs) == 0 {
		scriptList := buildScriptList(0)
		featureList := buildFeatureList(false, nil)
		lookupList, err := buildLookupList(nil)
		if err != nil {
			return nil, err
		}
		return assembleGSUB(scriptList, featureList, lookupList)
	}

	maxAlts := 0
	for _, p := range pairs {
		if len(p.alts) > maxAlts {
			maxAlts = len(p.alts)
		}
	}

	chunkCount := (len(pairs) + chainChunkSize - 1) / chainChunkSize
	if maxAlts+chunkCount > 65535 {
		return nil, fmt.Errorf("too many GSUB lookups: %d", maxAlts+chunkCount)
	}

	lookups := make([]gsubLookup, 0, maxAlts+chunkCount)
	stageLookupIndices := make([]uint16, maxAlts)

	// Stage lookups: stage k substitutes each origin with its k-th alternate.
	for stage := 0; stage < maxAlts; stage++ {
		coverage := make([]uint16, 0, len(pairs))
		replacements := make([]uint16, 0, len(pairs))
		for _, pair := range pairs {
			if stage < len(pair.alts) {
				coverage = append(coverage, pair.origin)
				replacements = append(replacements, pair.alts[stage])
			}
		}
		if len(coverage) == 0 {
			return nil, fmt.Errorf("internal error: empty stage %d", stage)
		}
		stageLookupIndices[stage] = uint16(len(lookups))
		lookups = append(lookups, gsubLookup{
			extensionType: 1, // SingleSubst
			subtable:      buildSingleSubst(coverage, replacements),
		})
	}

	// ChainContext lookups: one per chunk of origins.
	var featureLookupIndices []uint16
	for start := 0; start < len(pairs); start += chainChunkSize {
		end := start + chainChunkSize
		if end > len(pairs) {
			end = len(pairs)
		}
		chunk := pairs[start:end]

		subtable, err := buildChainContext(chunk, stageLookupIndices)
		if err != nil {
			return nil, err
		}
		featureLookupIndices = append(featureLookupIndices, uint16(len(lookups)))
		lookups = append(lookups, gsubLookup{
			extensionType: 6, // ChainContextSubst
			subtable:      subtable,
		})
	}

	lookupList, err := buildLookupList(lookups)
	if err != nil {
		return nil, err
	}

	scriptList := buildScriptList(1)
	featureList := buildFeatureList(true, featureLookupIndices)
	return assembleGSUB(scriptList, featureList, lookupList)
}

func assembleGSUB(scriptList, featureList, lookupList []byte) ([]byte, error) {
	out := make([]byte, 0, 10+len(scriptList)+len(featureList)+len(lookupList))
	out = appendU32(out, 0x00010000)

	scriptOffPos := len(out)
	out = appendU16(out, 0)
	featureOffPos := len(out)
	out = appendU16(out, 0)
	lookupOffPos := len(out)
	out = appendU16(out, 0)

	if err := setOffset16(out, scriptOffPos, len(out)); err != nil {
		return nil, err
	}
	out = append(out, scriptList...)

	if err := setOffset16(out, featureOffPos, len(out)); err != nil {
		return nil, err
	}
	out = append(out, featureList...)

	if err := setOffset16(out, lookupOffPos, len(out)); err != nil {
		return nil, err
	}
	out = append(out, lookupList...)

	return out, nil
}

func buildScriptList(featureCount int) []byte {
	out := make([]byte, 0, 20+2*featureCount)
	out = appendU16(out, 1) // scriptCount
	out = append(out, 'D', 'F', 'L', 'T')
	out = appendU16(out, 8) // Script offset from ScriptList start

	// Script
	out = appendU16(out, 4) // defaultLangSysOffset from Script start
	out = appendU16(out, 0) // langSysCount

	// LangSys
	out = appendU16(out, 0)      // lookupOrder
	out = appendU16(out, 0xFFFF) // reqFeatureIndex
	out = appendU16(out, uint16(featureCount))
	for i := 0; i < featureCount; i++ {
		out = appendU16(out, uint16(i))
	}
	return out
}

func buildFeatureList(hasFeature bool, lookupIndices []uint16) []byte {
	if !hasFeature {
		return appendU16(nil, 0)
	}

	out := make([]byte, 0, 8+4+2*len(lookupIndices))
	out = appendU16(out, 1) // featureCount
	out = append(out, 'c', 'a', 'l', 't')
	out = appendU16(out, 8) // Feature offset from FeatureList start

	// Feature
	out = appendU16(out, 0) // featureParamsOffset
	out = appendU16(out, uint16(len(lookupIndices)))
	for _, idx := range lookupIndices {
		out = appendU16(out, idx)
	}
	return out
}

func buildSingleSubst(coverage, replacements []uint16) []byte {
	n := len(coverage)
	out := make([]byte, 0, 6+2*n+4+2*n)
	out = appendU16(out, 2)             // substFormat
	out = appendU16(out, uint16(6+2*n)) // coverageOffset
	out = appendU16(out, uint16(n))     // glyphCount
	for _, gid := range replacements {
		out = appendU16(out, gid)
	}

	// Coverage format 1
	out = appendU16(out, 1)
	out = appendU16(out, uint16(n))
	for _, gid := range coverage {
		out = appendU16(out, gid)
	}
	return out
}

func buildChainContext(chunk []gsubPair, stageLookupIndices []uint16) ([]byte, error) {
	n := len(chunk)

	inputEntries := make([]classEntry, 0, n)
	backtrackEntries := make([]classEntry, 0, n*2)
	origins := make([]uint16, 0, n)
	ruleSets := make([][]gsubRule, 0, n)

	nextBacktrackClass := uint16(1)

	for i, pair := range chunk {
		inputClass := uint16(i + 1)
		inputEntries = append(inputEntries, classEntry{pair.origin, inputClass})
		origins = append(origins, pair.origin)

		originBacktrackClass := nextBacktrackClass
		nextBacktrackClass++
		backtrackEntries = append(backtrackEntries, classEntry{pair.origin, originBacktrackClass})

		altClasses := make([]uint16, 0, len(pair.alts))
		for _, alt := range pair.alts {
			class := nextBacktrackClass
			nextBacktrackClass++
			altClasses = append(altClasses, class)
			backtrackEntries = append(backtrackEntries, classEntry{alt, class})
		}

		rules := make([]gsubRule, 0, len(pair.alts)+1)
		rules = append(rules, gsubRule{
			backtrack: originBacktrackClass,
			lookup:    stageLookupIndices[0],
		})
		for stage := 1; stage < len(pair.alts); stage++ {
			rules = append(rules, gsubRule{
				backtrack: altClasses[stage-1],
				lookup:    stageLookupIndices[stage],
			})
		}
		rules = append(rules, gsubRule{
			backtrack: altClasses[len(altClasses)-1],
			lookup:    stageLookupIndices[0],
		})
		ruleSets = append(ruleSets, rules)
	}

	out := make([]byte, 0, 12+2*(n+1)+2*len(origins)+12*len(backtrackEntries)+16*n)

	out = appendU16(out, 2) // format

	coverageOffPos := len(out)
	out = appendU16(out, 0)
	backtrackOffPos := len(out)
	out = appendU16(out, 0)
	inputOffPos := len(out)
	out = appendU16(out, 0)
	lookaheadOffPos := len(out)
	out = appendU16(out, 0)

	out = appendU16(out, uint16(n+1)) // ruleSetCount
	ruleSetOffPos := len(out)
	for i := 0; i <= n; i++ {
		out = appendU16(out, 0)
	}

	// Coverage
	if err := setOffset16(out, coverageOffPos, len(out)); err != nil {
		return nil, err
	}
	out = appendCoverage(out, origins)

	// Backtrack class definition
	if err := setOffset16(out, backtrackOffPos, len(out)); err != nil {
		return nil, err
	}
	out = appendClassDef(out, backtrackEntries)

	// Input class definition
	if err := setOffset16(out, inputOffPos, len(out)); err != nil {
		return nil, err
	}
	out = appendClassDef(out, inputEntries)

	// Lookahead class definition (empty)
	if err := setOffset16(out, lookaheadOffPos, len(out)); err != nil {
		return nil, err
	}
	out = appendClassDef(out, nil)

	// Rule sets (index 0 is NULL)
	for i := 1; i <= n; i++ {
		if err := setOffset16(out, ruleSetOffPos+2*i, len(out)); err != nil {
			return nil, err
		}
		var err error
		out, err = appendRuleSet(out, ruleSets[i-1])
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

func appendCoverage(out []byte, gids []uint16) []byte {
	out = appendU16(out, 1)
	out = appendU16(out, uint16(len(gids)))
	for _, gid := range gids {
		out = appendU16(out, gid)
	}
	return out
}

func appendClassDef(out []byte, entries []classEntry) []byte {
	sort.Slice(entries, func(i, j int) bool { return entries[i].gid < entries[j].gid })

	out = appendU16(out, 2) // classFormat
	out = appendU16(out, uint16(len(entries)))
	for _, e := range entries {
		out = appendU16(out, e.gid) // startGlyphID
		out = appendU16(out, e.gid) // endGlyphID
		out = appendU16(out, e.class)
	}
	return out
}

func appendRuleSet(out []byte, rules []gsubRule) ([]byte, error) {
	ruleSetStart := len(out)
	out = appendU16(out, uint16(len(rules)))
	offsetsPos := len(out)
	for range rules {
		out = appendU16(out, 0)
	}

	for i, rule := range rules {
		if err := setOffset16(out, offsetsPos+2*i, len(out)-ruleSetStart); err != nil {
			return nil, err
		}

		out = appendU16(out, 1) // backtrackGlyphCount
		out = appendU16(out, rule.backtrack)
		out = appendU16(out, 1) // inputGlyphCount (the current glyph)
		out = appendU16(out, 0) // lookaheadGlyphCount
		out = appendU16(out, 1) // seqLookupCount
		out = appendU16(out, 0) // sequenceIndex
		out = appendU16(out, rule.lookup)
	}

	return out, nil
}

func buildLookupList(lookups []gsubLookup) ([]byte, error) {
	if len(lookups) > 65535 {
		return nil, fmt.Errorf("too many GSUB lookups: %d", len(lookups))
	}

	out := make([]byte, 0, 2+2*len(lookups)+16*len(lookups))
	out = appendU16(out, uint16(len(lookups)))

	headerOffPos := len(out)
	for range lookups {
		out = appendU16(out, 0)
	}

	stubOffsets := make([]int, len(lookups))
	stubOffPositions := make([]int, len(lookups))

	// Place every Lookup table and its extension stub near the start. The
	// actual subtables are appended afterwards and referenced with 32-bit
	// offsets from the stubs.
	for i, lookup := range lookups {
		lookupOffset := len(out)
		if err := setOffset16(out, headerOffPos+2*i, lookupOffset); err != nil {
			return nil, err
		}

		out = appendU16(out, 7) // lookupType: ExtensionSubst
		out = appendU16(out, 0) // lookupFlag
		out = appendU16(out, 1) // subTableCount
		out = appendU16(out, 8) // subtable offset from Lookup start (header + one offset)

		stubOffsets[i] = len(out)
		out = appendU16(out, 1) // extensionFormat
		out = appendU16(out, lookup.extensionType)
		stubOffPositions[i] = len(out)
		out = appendU32(out, 0)
	}

	for i, lookup := range lookups {
		if len(out)%2 != 0 {
			out = append(out, 0)
		}
		farOffset := len(out)
		out = append(out, lookup.subtable...)

		delta := farOffset - stubOffsets[i]
		if delta < 0 {
			return nil, fmt.Errorf("internal error: negative extension offset")
		}
		putU32(out, stubOffPositions[i], uint32(delta))
	}

	return out, nil
}

// setOffset16 writes a 16-bit offset at pos, rejecting values that do not fit.
func setOffset16(out []byte, pos, value int) error {
	if value < 0 || value > 0xFFFF {
		return fmt.Errorf("OpenType offset overflow: %d", value)
	}
	putU16(out, pos, uint16(value))
	return nil
}
