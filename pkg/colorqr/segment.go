package colorqr

import (
	"errors"
	"fmt"
)

// Mode is a QR data segment mode.
type Mode int

const (
	ModeNumeric      Mode = 0b0001
	ModeAlphanumeric Mode = 0b0010
	ModeByte         Mode = 0b0100
	modeECI          Mode = 0b0111
	modeKanji        Mode = 0b1000
)

func (m Mode) String() string {
	switch m {
	case ModeNumeric:
		return "numeric"
	case ModeAlphanumeric:
		return "alphanumeric"
	case ModeByte:
		return "byte"
	}
	return fmt.Sprintf("Mode(%04b)", int(m))
}

const alnumChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

// countBits returns the width of the character count indicator. HCC2D
// (Grillo et al., IMCSIT 2010) widens QR's indicator because colored symbols
// hold more characters: byte mode always uses 16 bits, and the other modes
// add BpM/2 bits (rounded up) to the QR width.
func countBits(m Mode, version int, s Scheme) int {
	if m == ModeByte {
		return 16
	}
	var qr [3]int
	switch m {
	case ModeNumeric:
		qr = [3]int{10, 12, 14}
	case ModeAlphanumeric:
		qr = [3]int{9, 11, 13}
	case modeKanji:
		qr = [3]int{8, 10, 12}
	default:
		return 0
	}
	i := 0
	if version >= 27 {
		i = 2
	} else if version >= 10 {
		i = 1
	}
	return qr[i] + (s.BitsPerModule()+1)/2
}

// chooseMode picks the most compact single segment mode for data.
func chooseMode(data []byte) Mode {
	numeric, alnum := true, true
	for _, b := range data {
		if b < '0' || b > '9' {
			numeric = false
		}
		if indexByte(alnumChars, b) < 0 {
			alnum = false
		}
	}
	switch {
	case numeric && len(data) > 0:
		return ModeNumeric
	case alnum && len(data) > 0:
		return ModeAlphanumeric
	}
	return ModeByte
}

func indexByte(s string, b byte) int {
	for i := range len(s) {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// segmentBits returns the size of a single segment holding data in mode m.
func segmentBits(m Mode, n, version int, s Scheme) int {
	head := 4 + countBits(m, version, s)
	switch m {
	case ModeNumeric:
		return head + n/3*10 + [3]int{0, 4, 7}[n%3]
	case ModeAlphanumeric:
		return head + n/2*11 + n%2*6
	}
	return head + 8*n
}

func maxCount(m Mode, version int, s Scheme) int { return 1<<countBits(m, version, s) - 1 }

func writeSegment(w *bitWriter, m Mode, data []byte, version int, s Scheme) {
	w.write(uint32(m), 4)
	w.write(uint32(len(data)), countBits(m, version, s))
	switch m {
	case ModeNumeric:
		for i := 0; i < len(data); i += 3 {
			n := min(3, len(data)-i)
			v := 0
			for _, c := range data[i : i+n] {
				v = v*10 + int(c-'0')
			}
			w.write(uint32(v), [4]int{0, 4, 7, 10}[n])
		}
	case ModeAlphanumeric:
		for i := 0; i < len(data); i += 2 {
			v := indexByte(alnumChars, data[i])
			if i+1 < len(data) {
				w.write(uint32(v*45+indexByte(alnumChars, data[i+1])), 11)
			} else {
				w.write(uint32(v), 6)
			}
		}
	default:
		for _, b := range data {
			w.write(uint32(b), 8)
		}
	}
}

// readSegments parses QR data segments up to the terminator or the end of
// the data. ECI designators are skipped (the payload is returned as raw
// bytes).
func readSegments(data []byte, version int, s Scheme) ([]byte, error) {
	r := bitReader{buf: data}
	left := func() int { return 8*len(data) - r.n }
	var out []byte
	for left() >= 4 {
		m := Mode(r.read(4))
		if m == 0 {
			break // terminator
		}
		if m == modeECI {
			if left() < 8 {
				return nil, errors.New("truncated ECI designator")
			}
			switch first := r.read(8); {
			case first&0x80 == 0:
			case first&0xc0 == 0x80:
				r.read(8)
			case first&0xe0 == 0xc0:
				r.read(16)
			default:
				return nil, errors.New("invalid ECI designator")
			}
			continue
		}
		cb := countBits(m, version, s)
		if cb == 0 || m == modeKanji {
			return nil, fmt.Errorf("unsupported segment mode %04b", int(m))
		}
		if left() < cb {
			return nil, errors.New("truncated segment header")
		}
		n := int(r.read(cb))
		if segmentBits(m, n, version, s)-4-cb > left() {
			return nil, fmt.Errorf("%s segment of %d characters exceeds symbol capacity", m, n)
		}
		switch m {
		case ModeNumeric:
			for i := 0; i < n; i += 3 {
				k := min(3, n-i)
				v := int(r.read([4]int{0, 4, 7, 10}[k]))
				digits := make([]byte, k)
				for j := k - 1; j >= 0; j-- {
					digits[j] = byte('0' + v%10)
					v /= 10
				}
				if v != 0 {
					return nil, errors.New("invalid numeric segment")
				}
				out = append(out, digits...)
			}
		case ModeAlphanumeric:
			for i := 0; i < n; i += 2 {
				if i+1 < n {
					v := int(r.read(11))
					if v >= 45*45 {
						return nil, errors.New("invalid alphanumeric segment")
					}
					out = append(out, alnumChars[v/45], alnumChars[v%45])
				} else {
					v := int(r.read(6))
					if v >= 45 {
						return nil, errors.New("invalid alphanumeric segment")
					}
					out = append(out, alnumChars[v])
				}
			}
		default:
			for range n {
				out = append(out, byte(r.read(8)))
			}
		}
	}
	return out, nil
}
