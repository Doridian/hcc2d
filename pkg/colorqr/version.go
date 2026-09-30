package colorqr

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
)

const (
	MinVersion = 1
	MaxVersion = 40
)

// sideForVersion returns the number of modules per side, as in QR.
func sideForVersion(v int) int { return 17 + 4*v }

// ECLevel is the error correction level. As in QR, it is stored in the
// format information, but the RS block layout is derived from the symbol's
// actual codeword count (which grows with the number of colors) rather than
// from the QR tables.
type ECLevel int

const (
	ECLow      ECLevel = iota // ~20% of codewords are parity; corrects ~10% byte errors
	ECMedium                  // ~38%; corrects ~19%
	ECQuartile                // ~55%; corrects ~27%
	ECHigh                    // ~65%; corrects ~32%
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

// parityRatio mirrors the share of EC codewords QR spends at each level.
func (l ECLevel) parityRatio() float64 {
	return [...]float64{0.20, 0.38, 0.55, 0.65}[l]
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
// QR format information is 5 data bits (EC level, mask) protected by a
// BCH(15,5) code and XORed with 0x5412. HCC2D keeps it black and white; we
// additionally signal the color scheme by using a different XOR mask for the
// 8-color scheme. With 0x544d, every 8-color format word is at least distance
// 5 (the code's covering radius) from every 4-color one, so the two sets stay
// separable with up to two bit errors. With three, the decoder tries every
// nearby word and lets Reed-Solomon pick (see formatCandidates).

const (
	formatMask4 = 0x5412
	formatMask8 = 0x544d
)

func bchFormat(data uint32) uint32 {
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return data<<10 | rem&0x3ff
}

type formatInfo struct {
	scheme Scheme
	level  ECLevel
	mask   int
}

func (f formatInfo) bits() uint32 {
	m := uint32(formatMask4)
	if f.scheme == EightColor {
		m = formatMask8
	}
	return bchFormat(f.level.formatBits()<<3|uint32(f.mask)) ^ m
}

// decodeFormat returns the format word nearest to raw and its Hamming
// distance.
func decodeFormat(raw uint32) (formatInfo, int) {
	var best formatInfo
	bestDist := 99
	for _, s := range []Scheme{FourColor, EightColor} {
		for l := ECLow; l <= ECHigh; l++ {
			for mask := range 8 {
				f := formatInfo{s, l, mask}
				if d := bits.OnesCount32(f.bits() ^ raw); d < bestDist {
					best, bestDist = f, d
				}
			}
		}
	}
	return best, bestDist
}

// formatCandidates returns every format word within maxDist of either raw
// copy, nearest first.
func formatCandidates(raws [2]uint32, maxDist int) []formatInfo {
	type cand struct {
		f formatInfo
		d int
	}
	var cs []cand
	for _, s := range []Scheme{FourColor, EightColor} {
		for l := ECLow; l <= ECHigh; l++ {
			for mask := range 8 {
				f := formatInfo{s, l, mask}
				w := f.bits()
				d := min(bits.OnesCount32(w^raws[0]), bits.OnesCount32(w^raws[1]))
				if d <= maxDist {
					cs = append(cs, cand{f, d})
				}
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].d < cs[j].d })
	out := make([]formatInfo, len(cs))
	for i, c := range cs {
		out[i] = c.f
	}
	return out
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
// Reed-Solomon blocks. Blocks are at most 255 bytes (the RS limit for
// GF(256)); the first numShort blocks are one byte shorter than the rest,
// and every block carries the same number of parity bytes, as in QR.
type blockLayout struct {
	total     int // total codewords in the symbol
	numBlocks int
	numShort  int
	shortLen  int // length of a short block (data + parity)
	parity    int // parity bytes per block
}

func newBlockLayout(total int, level ECLevel) (blockLayout, error) {
	if total < 4 {
		return blockLayout{}, fmt.Errorf("symbol too small: %d codewords", total)
	}
	nb := (total + 254) / 255
	b := blockLayout{
		total:     total,
		numBlocks: nb,
		shortLen:  total / nb,
		numShort:  nb - total%nb,
	}
	b.parity = int(math.Round(float64(b.shortLen) * level.parityRatio()))
	b.parity = max(b.parity, 2)
	b.parity = min(b.parity, b.shortLen-1)
	return b, nil
}

func (b blockLayout) blockLen(i int) int {
	if i < b.numShort {
		return b.shortLen
	}
	return b.shortLen + 1
}

func (b blockLayout) dataCapacity() int { return b.total - b.numBlocks*b.parity }

// interleave splits data into blocks, computes parity for each, and
// interleaves them as ISO/IEC 18004 does: first byte of every block, then
// the second, …, followed by the parity bytes in the same fashion. A damaged
// area of the symbol is thereby spread across all blocks.
func (b blockLayout) interleave(data []byte) []byte {
	dataBlocks := make([][]byte, b.numBlocks)
	parity := make([][]byte, b.numBlocks)
	off := 0
	for i := range dataBlocks {
		n := b.blockLen(i) - b.parity
		dataBlocks[i] = data[off : off+n]
		parity[i] = rsEncode(dataBlocks[i], b.parity)
		off += n
	}
	out := make([]byte, 0, b.total)
	for i := 0; i <= b.shortLen-b.parity; i++ {
		for _, d := range dataBlocks {
			if i < len(d) {
				out = append(out, d[i])
			}
		}
	}
	for i := 0; i < b.parity; i++ {
		for _, p := range parity {
			out = append(out, p[i])
		}
	}
	return out
}

// deinterleave reverses interleave and error-corrects every block. It
// returns the data bytes and the number of corrected bytes.
func (b blockLayout) deinterleave(cw []byte) ([]byte, int, error) {
	blocks := make([][]byte, b.numBlocks)
	for i := range blocks {
		blocks[i] = make([]byte, b.blockLen(i))
	}
	idx := 0
	for i := 0; i <= b.shortLen-b.parity; i++ {
		for _, blk := range blocks {
			if i < len(blk)-b.parity {
				blk[i] = cw[idx]
				idx++
			}
		}
	}
	for i := 0; i < b.parity; i++ {
		for _, blk := range blocks {
			blk[len(blk)-b.parity+i] = cw[idx]
			idx++
		}
	}
	var data []byte
	corrected := 0
	for i, blk := range blocks {
		n, err := rsDecode(blk, b.parity)
		if err != nil {
			return nil, 0, fmt.Errorf("block %d/%d: %w", i+1, b.numBlocks, err)
		}
		corrected += n
		data = append(data, blk[:len(blk)-b.parity]...)
	}
	return data, corrected, nil
}
