package colorqr

import "errors"

// Reed-Solomon over GF(2^8) with the QR code field polynomial
// x^8 + x^4 + x^3 + x^2 + 1 (0x11d) and generator α = 2. The generator
// polynomial of an n-symbol code has the roots α^0 … α^(n-1), exactly as in
// ISO/IEC 18004.
//
// The decoder locates errors itself (Berlekamp-Massey + Chien search +
// Forney) rather than relying on known erasure positions, so a block with n
// parity bytes corrects up to n/2 misclassified bytes at unknown positions.

var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < len(gfExp); i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func gfDiv(a, b byte) byte {
	if b == 0 {
		panic("gf256: division by zero")
	}
	if a == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+255-int(gfLog[b])]
}

// gfPow returns α^e for any integer e.
func gfPow(e int) byte {
	e %= 255
	if e < 0 {
		e += 255
	}
	return gfExp[e]
}

// rsGenerator returns the generator polynomial of degree n, highest-order
// coefficient first (the leading 1 is omitted).
func rsGenerator(n int) []byte {
	g := make([]byte, n)
	g[n-1] = 1 // start with the polynomial "1" (stored without the leading term)
	root := byte(1)
	for i := 0; i < n; i++ {
		// multiply by (x - α^i)
		for j := 0; j < n; j++ {
			g[j] = gfMul(g[j], root)
			if j+1 < n {
				g[j] ^= g[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return g
}

// rsEncode returns the n parity bytes for data.
func rsEncode(data []byte, n int) []byte {
	gen := rsGenerator(n)
	rem := make([]byte, n)
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		for i := range rem {
			rem[i] ^= gfMul(gen[i], factor)
		}
	}
	return rem
}

var errRSUncorrectable = errors.New("reed-solomon: too many errors")

// rsDecode corrects block (data followed by n parity bytes) in place and
// returns the number of corrected bytes.
func rsDecode(block []byte, n int) (int, error) {
	synd := make([]byte, n)
	clean := true
	for i := range synd {
		synd[i] = polyEvalHighFirst(block, gfPow(i))
		if synd[i] != 0 {
			clean = false
		}
	}
	if clean {
		return 0, nil
	}

	// Berlekamp-Massey: error locator Λ(x), lowest-order coefficient first.
	lambda := []byte{1}
	prev := []byte{1}
	L, m := 0, 1
	b := byte(1)
	for k := 0; k < n; k++ {
		d := synd[k]
		for i := 1; i <= L && i < len(lambda); i++ {
			d ^= gfMul(lambda[i], synd[k-i])
		}
		if d == 0 {
			m++
			continue
		}
		coef := gfDiv(d, b)
		next := make([]byte, max(len(lambda), len(prev)+m))
		copy(next, lambda)
		for i, p := range prev {
			next[i+m] ^= gfMul(coef, p)
		}
		if 2*L <= k {
			prev = lambda
			L = k + 1 - L
			b = d
			m = 1
		} else {
			m++
		}
		lambda = next
	}
	lambda = lambda[:L+1]
	if 2*L > n {
		return 0, errRSUncorrectable
	}

	// Chien search: Λ(X^-1) = 0 for every error locator X = α^p, where p is
	// the power of x at the erroneous position.
	N := len(block)
	var positions []int
	for p := 0; p < N; p++ {
		if polyEvalLowFirst(lambda, gfPow(-p)) == 0 {
			positions = append(positions, p)
		}
	}
	if len(positions) != L {
		return 0, errRSUncorrectable
	}

	// Forney: Ω(x) = S(x)Λ(x) mod x^n, e = X·Ω(X^-1)/Λ'(X^-1).
	omega := make([]byte, n)
	for i := 0; i < n; i++ {
		for j := 0; j <= i && j < len(lambda); j++ {
			omega[i] ^= gfMul(synd[i-j], lambda[j])
		}
	}
	deriv := make([]byte, len(lambda))
	for i := 1; i < len(lambda); i += 2 {
		deriv[i-1] = lambda[i]
	}
	for _, p := range positions {
		xInv := gfPow(-p)
		den := polyEvalLowFirst(deriv, xInv)
		if den == 0 {
			return 0, errRSUncorrectable
		}
		e := gfMul(gfPow(p), gfDiv(polyEvalLowFirst(omega, xInv), den))
		block[N-1-p] ^= e
	}

	for i := 0; i < n; i++ {
		if polyEvalHighFirst(block, gfPow(i)) != 0 {
			return 0, errRSUncorrectable
		}
	}
	return L, nil
}

func polyEvalHighFirst(p []byte, x byte) byte {
	var y byte
	for _, c := range p {
		y = gfMul(y, x) ^ c
	}
	return y
}

func polyEvalLowFirst(p []byte, x byte) byte {
	var y byte
	for i := len(p) - 1; i >= 0; i-- {
		y = gfMul(y, x) ^ p[i]
	}
	return y
}
