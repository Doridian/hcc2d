package colorqr

import (
	"fmt"

	"github.com/klauspost/reedsolomon"
)

const (
	// RSDataShards is the number of data bytes per RS block.
	RSDataShards = 32
	// RSParityShards is the number of parity bytes per RS block.
	RSParityShards = 8
	rsBlockSize    = RSDataShards + RSParityShards // 40 bytes total per block
)

// rsEncoder is a package-level encoder; New is cheap but cached for clarity.
var rsEncoder reedsolomon.Encoder

func init() {
	var err error
	rsEncoder, err = reedsolomon.New(RSDataShards, RSParityShards)
	if err != nil {
		panic(fmt.Sprintf("reedsolomon.New: %v", err))
	}
}

// rsEncode applies Reed-Solomon ECC to data.
// The output is laid out as contiguous 40-byte blocks:
//
//	[32 data bytes][8 parity bytes] per block
//
// The input is zero-padded to a multiple of RSDataShards before encoding;
// the original length is preserved by the caller-supplied 4-byte header.
func rsEncode(data []byte) ([]byte, error) {
	padded := padToMultiple(data, RSDataShards)
	if len(padded) == 0 {
		padded = make([]byte, RSDataShards)
	}
	nBlocks := len(padded) / RSDataShards
	out := make([]byte, nBlocks*rsBlockSize)

	for b := 0; b < nBlocks; b++ {
		src := padded[b*RSDataShards : (b+1)*RSDataShards]
		shards := make([][]byte, RSDataShards+RSParityShards)
		for i := 0; i < RSDataShards; i++ {
			shards[i] = []byte{src[i]}
		}
		for i := RSDataShards; i < RSDataShards+RSParityShards; i++ {
			shards[i] = []byte{0}
		}
		if err := rsEncoder.Encode(shards); err != nil {
			return nil, fmt.Errorf("rs encode block %d: %w", b, err)
		}
		dst := out[b*rsBlockSize:]
		for i, s := range shards {
			dst[i] = s[0]
		}
	}
	return out, nil
}

// rsDecode recovers data from RS-encoded bytes.
//
// byteConf holds a confidence value per encoded byte (lower = more confident;
// it is the YUV-space squared distance from the cell's sampled color to the
// nearest palette entry).  Bytes whose confidence exceeds erasureThreshold
// are treated as erasures and reconstructed by RS.
//
// The data cell count in the grid may exceed the encoded length by a few
// bytes of trailing zero-pad; we truncate to the largest complete RS block.
//
// Returns the decoded (possibly padded) bytes; callers strip padding via the
// length header they stored in the first four bytes.
func rsDecode(encoded []byte, byteConf []float64) ([]byte, error) {
	nBlocks := len(encoded) / rsBlockSize
	if nBlocks == 0 {
		return nil, fmt.Errorf("encoded data too short for one RS block (%d bytes)", len(encoded))
	}
	// Truncate trailing zero-pad from unwritten data cells.
	encoded = encoded[:nBlocks*rsBlockSize]
	out := make([]byte, nBlocks*RSDataShards)

	for b := 0; b < nBlocks; b++ {
		src := encoded[b*rsBlockSize : (b+1)*rsBlockSize]

		shards := make([][]byte, RSDataShards+RSParityShards)
		for i := 0; i < rsBlockSize; i++ {
			var conf float64
			if b*rsBlockSize+i < len(byteConf) {
				conf = byteConf[b*rsBlockSize+i]
			}
			if conf > erasureThreshold {
				shards[i] = nil // mark as erasure
			} else {
				shards[i] = []byte{src[i]}
			}
		}

		if err := rsEncoder.ReconstructData(shards); err != nil {
			return nil, fmt.Errorf("rs reconstruct block %d: %w", b, err)
		}
		dst := out[b*RSDataShards:]
		for i := 0; i < RSDataShards; i++ {
			if shards[i] != nil {
				dst[i] = shards[i][0]
			}
		}
	}
	return out, nil
}

// erasureThreshold is the squared YUV distance above which a cell is treated
// as an erasure.  Typical inter-palette distance is ~150,000; ambiguous reads
// sit around 10,000–40,000.
const erasureThreshold = 8000.0

// padToMultiple zero-pads b to the next multiple of n.
func padToMultiple(b []byte, n int) []byte {
	rem := len(b) % n
	if rem == 0 {
		return b
	}
	padded := make([]byte, len(b)+n-rem)
	copy(padded, b)
	return padded
}
