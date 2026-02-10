package dsp

// VP8 loop filter implementations matching dec.c.
// Both "simple" and "complex" (a.k.a. "normal") filters are provided.
//
// All public filter functions use a full-buffer + base-offset approach so that
// "negative-context" access (e.g. p[off-2*stride]) always resolves to a valid
// non-negative index within the buffer. This avoids the negative slice index
// panic that would occur with C-style pointer arithmetic in Go.

// needsFilter returns true if the edge pixels need filtering.
// Matches C: 4 * VP8kabs0[p0-q0] + VP8kabs0[p1-q1] <= t
func needsFilter(p1, p0, q0, q1 int, thresh int) bool {
	return 4*int(Kabs0(p0-q0))+int(Kabs0(p1-q1)) <= thresh
}

// needsFilter2 extends needsFilter for the complex (6-tap) filter.
func needsFilter2(p3, p2, p1, p0, q0, q1, q2, q3 int, thresh, ithresh int) bool {
	if !needsFilter(p1, p0, q0, q1, thresh) {
		return false
	}
	return int(Kabs0(p3-p2)) <= ithresh &&
		int(Kabs0(p2-p1)) <= ithresh &&
		int(Kabs0(p1-p0)) <= ithresh &&
		int(Kabs0(q3-q2)) <= ithresh &&
		int(Kabs0(q2-q1)) <= ithresh &&
		int(Kabs0(q1-q0)) <= ithresh
}

// hev returns true if there is a high edge variance between p1-p0 and q1-q0.
func hev(p1, p0, q0, q1 int, hevThresh int) bool {
	return int(Kabs0(p1-p0)) > hevThresh || int(Kabs0(q1-q0)) > hevThresh
}

// doFilter2 applies the 2-tap filter to a single edge sample.
// Matches C DoFilter2_C: a = 3*(q0-p0) + sclip1(p1-q1), updates p0 and q0.
func doFilter2(p []byte, off, step int) {
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])

	a := 3*(q0-p0) + int(Ksclip1(int(p1)-int(q1)))
	a1 := int(Ksclip2((a + 4) >> 3))
	a2 := int(Ksclip2((a + 3) >> 3))
	p[off-step] = Kclip1(int(p0) + a2)
	p[off] = Kclip1(int(q0) - a1)
}

// doFilter4 applies the 4-tap normal filter to a single edge sample.
// Matches C DoFilter4_C: a = 3*(q0-p0) (NO p1-q1 term), updates p1, p0, q0, q1.
func doFilter4(p []byte, off, step int) {
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])

	a := 3 * (q0 - p0)
	a1 := int(Ksclip2((a + 4) >> 3))
	a2 := int(Ksclip2((a + 3) >> 3))
	a3 := (a1 + 1) >> 1
	p[off-2*step] = Kclip1(p1 + a3)
	p[off-step] = Kclip1(p0 + a2)
	p[off] = Kclip1(q0 - a1)
	p[off+step] = Kclip1(q1 - a3)
}

// doFilter6 applies the 6-tap complex filter to a single edge sample.
func doFilter6(p []byte, off, step int) {
	p2 := int(p[off-3*step])
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])
	q2 := int(p[off+2*step])

	a := int(Ksclip1(3*(q0-p0) + int(Ksclip1(p1-q1))))
	a1 := (27*a + 63) >> 7
	a2 := (18*a + 63) >> 7
	a3 := (9*a + 63) >> 7
	p[off-3*step] = Kclip1(p2 + a3)
	p[off-2*step] = Kclip1(p1 + a2)
	p[off-step] = Kclip1(p0 + a1)
	p[off] = Kclip1(q0 - a1)
	p[off+step] = Kclip1(q1 - a2)
	p[off+2*step] = Kclip1(q2 - a3)
}

// ---------- Simple filter ----------

// SimpleVFilter16 applies the simple loop filter vertically across a 16-wide edge.
// p is the full buffer, base is the offset of the edge row within p.
func SimpleVFilter16(p []byte, base, stride, thresh int) {
	thresh2 := 2*thresh + 1
	for i := 0; i < 16; i++ {
		off := base + i
		p1 := int(p[off-2*stride])
		p0 := int(p[off-stride])
		q0 := int(p[off])
		q1 := int(p[off+stride])
		if needsFilter(p1, p0, q0, q1, thresh2) {
			doFilter2(p, off, stride)
		}
	}
}

// SimpleHFilter16 applies the simple loop filter horizontally across a 16-high edge.
// p is the full buffer, base is the offset of the edge column within p.
func SimpleHFilter16(p []byte, base, stride, thresh int) {
	thresh2 := 2*thresh + 1
	for i := 0; i < 16; i++ {
		off := base + i*stride
		p1 := int(p[off-2])
		p0 := int(p[off-1])
		q0 := int(p[off])
		q1 := int(p[off+1])
		if needsFilter(p1, p0, q0, q1, thresh2) {
			doFilter2(p, off, 1)
		}
	}
}

// SimpleVFilter16i applies simple vertical filtering at internal block boundaries
// (every 4 rows within a macroblock).
// p is the full buffer, base is the offset of the macroblock top-left within p.
func SimpleVFilter16i(p []byte, base, stride, thresh int) {
	for k := 1; k <= 3; k++ {
		SimpleVFilter16(p, base+k*4*stride, stride, thresh)
	}
}

// SimpleHFilter16i applies simple horizontal filtering at internal block boundaries.
// p is the full buffer, base is the offset of the macroblock top-left within p.
func SimpleHFilter16i(p []byte, base, stride, thresh int) {
	for k := 1; k <= 3; k++ {
		SimpleHFilter16(p, base+k*4, stride, thresh)
	}
}

// ---------- Complex (normal) filter ----------

// filterLoop26 implements FilterLoop26_C: macroblock edge filtering.
// HEV -> doFilter2, !HEV -> doFilter6.
func filterLoop26(p []byte, base, hstride, vstride, size, thresh, ithresh, hevT int) {
	thresh2 := 2*thresh + 1
	off := base
	for i := 0; i < size; i++ {
		p3 := int(p[off-4*hstride])
		p2 := int(p[off-3*hstride])
		p1 := int(p[off-2*hstride])
		p0 := int(p[off-hstride])
		q0 := int(p[off])
		q1 := int(p[off+hstride])
		q2 := int(p[off+2*hstride])
		q3 := int(p[off+3*hstride])
		if needsFilter2(p3, p2, p1, p0, q0, q1, q2, q3, thresh2, ithresh) {
			if hev(p1, p0, q0, q1, hevT) {
				doFilter2(p, off, hstride)
			} else {
				doFilter6(p, off, hstride)
			}
		}
		off += vstride
	}
}

// filterLoop24 implements FilterLoop24_C: inner edge filtering.
// HEV -> doFilter2, !HEV -> doFilter4.
func filterLoop24(p []byte, base, hstride, vstride, size, thresh, ithresh, hevT int) {
	thresh2 := 2*thresh + 1
	off := base
	for i := 0; i < size; i++ {
		p3 := int(p[off-4*hstride])
		p2 := int(p[off-3*hstride])
		p1 := int(p[off-2*hstride])
		p0 := int(p[off-hstride])
		q0 := int(p[off])
		q1 := int(p[off+hstride])
		q2 := int(p[off+2*hstride])
		q3 := int(p[off+3*hstride])
		if needsFilter2(p3, p2, p1, p0, q0, q1, q2, q3, thresh2, ithresh) {
			if hev(p1, p0, q0, q1, hevT) {
				doFilter2(p, off, hstride)
			} else {
				doFilter4(p, off, hstride)
			}
		}
		off += vstride
	}
}

// VFilter16 applies the complex vertical loop filter across a 16-wide edge.
// p is the full buffer, base is the offset of the edge row.
func VFilter16(p []byte, base, stride, thresh, ithresh, hevT int) {
	filterLoop26(p, base, stride, 1, 16, thresh, ithresh, hevT)
}

// HFilter16 applies the complex horizontal loop filter across a 16-high edge.
// p is the full buffer, base is the offset of the edge column.
func HFilter16(p []byte, base, stride, thresh, ithresh, hevT int) {
	filterLoop26(p, base, 1, stride, 16, thresh, ithresh, hevT)
}

// VFilter8 applies the complex vertical filter to an 8-wide chroma edge.
func VFilter8(u, v []byte, uBase, vBase, stride, thresh, ithresh, hevT int) {
	filterLoop26(u, uBase, stride, 1, 8, thresh, ithresh, hevT)
	filterLoop26(v, vBase, stride, 1, 8, thresh, ithresh, hevT)
}

// HFilter8 applies the complex horizontal filter to an 8-high chroma edge.
func HFilter8(u, v []byte, uBase, vBase, stride, thresh, ithresh, hevT int) {
	filterLoop26(u, uBase, 1, stride, 8, thresh, ithresh, hevT)
	filterLoop26(v, vBase, 1, stride, 8, thresh, ithresh, hevT)
}

// VFilter16i applies complex vertical filtering at internal block boundaries.
func VFilter16i(p []byte, base, stride, thresh, ithresh, hevT int) {
	for k := 1; k <= 3; k++ {
		filterLoop24(p, base+k*4*stride, stride, 1, 16, thresh, ithresh, hevT)
	}
}

// HFilter16i applies complex horizontal filtering at internal block boundaries.
func HFilter16i(p []byte, base, stride, thresh, ithresh, hevT int) {
	for k := 1; k <= 3; k++ {
		filterLoop24(p, base+k*4, 1, stride, 16, thresh, ithresh, hevT)
	}
}

// VFilter8i applies complex vertical filtering at internal 4-row boundaries
// for 8x8 chroma blocks.
func VFilter8i(u, v []byte, uBase, vBase, stride, thresh, ithresh, hevT int) {
	filterLoop24(u, uBase+4*stride, stride, 1, 8, thresh, ithresh, hevT)
	filterLoop24(v, vBase+4*stride, stride, 1, 8, thresh, ithresh, hevT)
}

// HFilter8i applies complex horizontal filtering at internal 4-column boundaries
// for 8x8 chroma blocks.
func HFilter8i(u, v []byte, uBase, vBase, stride, thresh, ithresh, hevT int) {
	filterLoop24(u, uBase+4, 1, stride, 8, thresh, ithresh, hevT)
	filterLoop24(v, vBase+4, 1, stride, 8, thresh, ithresh, hevT)
}
