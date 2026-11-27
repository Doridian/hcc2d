package colorqr

import "math"

// ── color classification ────────────────────────────────────────────────────

func nearest(c yuv, refs []yuv) int {
	best, bestD := 0, math.Inf(1)
	for i, r := range refs {
		if d := c.dist2(r); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func classifyNearest(samples, refs []yuv) []int {
	labels := make([]int, len(samples))
	for i, s := range samples {
		labels[i] = nearest(s, refs)
	}
	return labels
}

// classifyKMeans runs Lloyd's algorithm with k = palette size, starting from
// the palette-pattern colors so that cluster i stays associated with
// palette entry i.
func classifyKMeans(samples, refs []yuv) []int {
	cent := append([]yuv(nil), refs...)
	labels := classifyNearest(samples, cent)
	for range 30 {
		sums := make([]yuv, len(cent))
		counts := make([]int, len(cent))
		for i, s := range samples {
			k := labels[i]
			sums[k].Y += s.Y
			sums[k].U += s.U
			sums[k].V += s.V
			counts[k]++
		}
		for k := range cent {
			if counts[k] > 0 {
				n := float64(counts[k])
				cent[k] = yuv{sums[k].Y / n, sums[k].U / n, sums[k].V / n}
			}
		}
		next := classifyNearest(samples, cent)
		changed := false
		for i := range next {
			if next[i] != labels[i] {
				changed = true
				break
			}
		}
		labels = next
		if !changed {
			break
		}
	}
	return labels
}
