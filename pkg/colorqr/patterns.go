package colorqr

// finderCell returns true if position (dr,dc) within a 7×7 finder block
// should be dark (black).
//
// The QR finder pattern is:
//
//	███████
//	█     █
//	█ ███ █
//	█ ███ █
//	█ ███ █
//	█     █
//	███████
func finderCell(dr, dc int) bool {
	// outer ring
	if dr == 0 || dr == 6 || dc == 0 || dc == 6 {
		return true
	}
	// inner gap
	if dr == 1 || dr == 5 || dc == 1 || dc == 5 {
		return false
	}
	// inner square
	return true
}

// timingCell returns true if the timing pattern at index i should be dark.
// QR timing alternates dark/light starting from dark.
func timingCell(i int) bool {
	return i%2 == 0
}
