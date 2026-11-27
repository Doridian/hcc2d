package colorqr

import (
	"fmt"
	"math/bits"
)

const (
	MinVersion = 1
	MaxVersion = 40
)

// sideForVersion returns the number of modules per side, as in QR.
func sideForVersion(v int) int { return 17 + 4*v }

// ECLevel is the error correction level, stored in the format information
// exactly as in QR. HCC2D keeps QR's Reed-Solomon block structure: every QR
// block is repeated once per bit a module carries (see newBlockLayout), so
// each level corrects the same share of byte errors as in QR.
type ECLevel int

const (
	ECLow      ECLevel = iota // ~20% of codewords are parity (QR level L)
	ECMedium                  // ~38% (QR level M)
	ECQuartile                // ~55% (QR level Q)
	ECHigh                    // ~65% (QR level H)
)

func (l ECLevel) valid() bool { return l >= ECLow && l <= ECHigh }

func (l ECLevel) String() string {
	if !l.valid() {
		return fmt.Sprintf("ECLevel(%d)", int(l))
	}
	return "LMQH"[l : l+1]
}

// ParseECLevel parses "L", "M", "Q" or "H".
func ParseECLevel(s string) (ECLevel, error) {
	switch s {
	case "L", "l":
		return ECLow, nil
	case "M", "m":
		return ECMedium, nil
	case "Q", "q":
		return ECQuartile, nil
	case "H", "h":
		return ECHigh, nil
	}
	return 0, fmt.Errorf("unknown error correction level %q (want L, M, Q or H)", s)
}

// formatBits is the 2-bit EC indicator used in QR format information.
func (l ECLevel) formatBits() uint32 { return [...]uint32{1, 0, 3, 2}[l] }

// alignmentPositions is Table E.1 of ISO/IEC 18004.
var alignmentPositions = [MaxVersion + 1][]int{
	nil, nil,
	{6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34},
	{6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50}, {6, 30, 54}, {6, 32, 58}, {6, 34, 62},
	{6, 26, 46, 66}, {6, 26, 48, 70}, {6, 26, 50, 74}, {6, 30, 54, 78}, {6, 30, 56, 82}, {6, 30, 58, 86}, {6, 34, 62, 90},
	{6, 28, 50, 72, 94}, {6, 26, 50, 74, 98}, {6, 30, 54, 78, 102}, {6, 28, 54, 80, 106}, {6, 32, 58, 84, 110}, {6, 30, 58, 86, 114}, {6, 34, 62, 90, 118},
	{6, 26, 50, 74, 98, 122}, {6, 30, 54, 78, 102, 126}, {6, 26, 52, 78, 104, 130}, {6, 30, 56, 82, 108, 134}, {6, 34, 60, 86, 112, 138}, {6, 30, 58, 86, 114, 142}, {6, 34, 62, 90, 118, 146},
	{6, 30, 54, 78, 102, 126, 150}, {6, 24, 50, 76, 102, 128, 154}, {6, 28, 54, 80, 106, 132, 158}, {6, 32, 58, 84, 110, 136, 162}, {6, 26, 54, 82, 110, 138, 166}, {6, 30, 58, 86, 114, 142, 170},
}

// ── format information ──────────────────────────────────────────────────────
//
// HCC2D keeps QR's black-and-white format information unchanged: 5 data
// bits (EC level, mask) protected by a BCH(15,5) code and XORed with
// 0x5412. The color scheme is not part of it; the decoder recognizes it from
// the period of the Color Palette Patterns.

const formatMask = 0x5412

func bchFormat(data uint32) uint32 {
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return data<<10 | rem&0x3ff
}

type formatInfo struct {
	level ECLevel
	mask  int
}

func (f formatInfo) bits() uint32 {
	return bchFormat(f.level.formatBits()<<3|uint32(f.mask)) ^ formatMask
}

// decodeFormat returns the format word nearest to either raw copy and its
// Hamming distance. Format words are at least 7 apart, so up to three bit
// errors are corrected.
func decodeFormat(raws ...uint32) (formatInfo, int) {
	var best formatInfo
	bestDist := 99
	for l := ECLow; l <= ECHigh; l++ {
		for mask := range 8 {
			f := formatInfo{l, mask}
			for _, raw := range raws {
				if d := bits.OnesCount32(f.bits() ^ raw); d < bestDist {
					best, bestDist = f, d
				}
			}
		}
	}
	return best, bestDist
}

// ── version information (versions 7+) ──────────────────────────────────────

func versionBits(v int) uint32 {
	rem := uint32(v)
	for range 12 {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1f25)
	}
	return uint32(v)<<12 | rem&0xfff
}

func decodeVersion(raw uint32) (int, int) {
	best, bestDist := 0, 99
	for v := 7; v <= MaxVersion; v++ {
		if d := bits.OnesCount32(versionBits(v) ^ raw); d < bestDist {
			best, bestDist = v, d
		}
	}
	return best, bestDist
}

// ── codeword layout ─────────────────────────────────────────────────────────

// blockLayout describes how a symbol's codewords are split into
// Reed-Solomon blocks. HCC2D derives its code tables from QR's by
// multiplying the codeword counts by the bits per module (Grillo et al.,
// "High Capacity Colored Two Dimensional Codes", IMCSIT 2010); this
// implementation does that by repeating every QR block BpM times, which
// keeps QR's block sizes, its parity share and its correction capability.
// Short blocks come first and blocks are interleaved as in QR.
type blockLayout struct {
	dataLen []int // data codewords per block
	parity  int   // parity codewords per block (the same for every block)
}

func newBlockLayout(version int, level ECLevel, s Scheme) blockLayout {
	var b blockLayout
	for _, g := range qrBlocks[version][level] {
		b.parity = g.total - g.data
		for range g.count * s.BitsPerModule() {
			b.dataLen = append(b.dataLen, g.data)
		}
	}
	return b
}

func (b blockLayout) dataCapacity() int {
	n := 0
	for _, d := range b.dataLen {
		n += d
	}
	return n
}

func (b blockLayout) total() int { return b.dataCapacity() + len(b.dataLen)*b.parity }

// interleave splits data into blocks, computes parity for each, and
// interleaves them as ISO/IEC 18004 does: first byte of every block, then
// the second, …, followed by the parity bytes in the same fashion. A damaged
// area of the symbol is thereby spread across all blocks.
func (b blockLayout) interleave(data []byte) []byte {
	dataBlocks := make([][]byte, len(b.dataLen))
	parity := make([][]byte, len(b.dataLen))
	off, maxLen := 0, 0
	for i, n := range b.dataLen {
		dataBlocks[i] = data[off : off+n]
		parity[i] = rsEncode(dataBlocks[i], b.parity)
		off += n
		maxLen = max(maxLen, n)
	}
	out := make([]byte, 0, b.total())
	for i := range maxLen {
		for _, d := range dataBlocks {
			if i < len(d) {
				out = append(out, d[i])
			}
		}
	}
	for i := range b.parity {
		for _, p := range parity {
			out = append(out, p[i])
		}
	}
	return out
}

// deinterleave reverses interleave and error-corrects every block. It
// returns the data bytes and the number of corrected bytes.
func (b blockLayout) deinterleave(cw []byte) ([]byte, int, error) {
	blocks := make([][]byte, len(b.dataLen))
	maxLen := 0
	for i, n := range b.dataLen {
		blocks[i] = make([]byte, n+b.parity)
		maxLen = max(maxLen, n)
	}
	idx := 0
	for i := range maxLen {
		for j, blk := range blocks {
			if i < b.dataLen[j] {
				blk[i] = cw[idx]
				idx++
			}
		}
	}
	for i := range b.parity {
		for j, blk := range blocks {
			blk[b.dataLen[j]+i] = cw[idx]
			idx++
		}
	}
	var data []byte
	corrected := 0
	for i, blk := range blocks {
		n, err := rsDecode(blk, b.parity)
		if err != nil {
			return nil, 0, fmt.Errorf("block %d/%d: %w", i+1, len(blocks), err)
		}
		corrected += n
		data = append(data, blk[:b.dataLen[i]]...)
	}
	return data, corrected, nil
}
