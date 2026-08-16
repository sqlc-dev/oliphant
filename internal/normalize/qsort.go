// pg_qsort (src/port/qsort.c via lib/sort_template.h): the Bentley &
// McIlroy quicksort PostgreSQL uses. fill_in_constant_lengths sorts the
// constant records with it, and the sort is not stable — for duplicate
// locations (a MultiAssignRef source walked once per target column), WHICH
// duplicate ends up first decides the parameter number the constant gets
// (e.g. "SET (c,b,a) = ($1, b+$4, DEFAULT)"). Byte-parity therefore needs
// the exact algorithm, not just the same ordering criterion.
package normalize

// locLess is comp_location: compare by location only.
func locCompare(a, b constLoc) int {
	switch {
	case a.location < b.location:
		return -1
	case a.location > b.location:
		return +1
	default:
		return 0
	}
}

func med3(a []constLoc, x, y, z int) int {
	if locCompare(a[x], a[y]) < 0 {
		if locCompare(a[y], a[z]) < 0 {
			return y
		}
		if locCompare(a[x], a[z]) < 0 {
			return z
		}
		return x
	}
	if locCompare(a[y], a[z]) > 0 {
		return y
	}
	if locCompare(a[x], a[z]) < 0 {
		return x
	}
	return z
}

func swapN(a []constLoc, x, y, n int) {
	for i := 0; i < n; i++ {
		a[x+i], a[y+i] = a[y+i], a[x+i]
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// pgQsort sorts a[0:n] exactly as ST_SORT in sort_template.h.
func pgQsort(a []constLoc) {
	n := len(a)
loop:
	if n < 7 {
		for pm := 1; pm < n; pm++ {
			for pl := pm; pl > 0 && locCompare(a[pl-1], a[pl]) > 0; pl-- {
				a[pl], a[pl-1] = a[pl-1], a[pl]
			}
		}
		return
	}
	presorted := true
	for pm := 1; pm < n; pm++ {
		if locCompare(a[pm-1], a[pm]) > 0 {
			presorted = false
			break
		}
	}
	if presorted {
		return
	}
	pm := n / 2
	if n > 7 {
		pl := 0
		pn := n - 1
		if n > 40 {
			d := n / 8
			pl = med3(a, pl, pl+d, pl+2*d)
			pm = med3(a, pm-d, pm, pm+d)
			pn = med3(a, pn-2*d, pn-d, pn)
		}
		pm = med3(a, pl, pm, pn)
	}
	a[0], a[pm] = a[pm], a[0]
	pa, pb := 1, 1
	pc, pd := n-1, n-1
	for {
		var r int
		for pb <= pc {
			if r = locCompare(a[pb], a[0]); r > 0 {
				break
			}
			if r == 0 {
				a[pa], a[pb] = a[pb], a[pa]
				pa++
			}
			pb++
		}
		for pb <= pc {
			if r = locCompare(a[pc], a[0]); r < 0 {
				break
			}
			if r == 0 {
				a[pc], a[pd] = a[pd], a[pc]
				pd--
			}
			pc--
		}
		if pb > pc {
			break
		}
		a[pb], a[pc] = a[pc], a[pb]
		pb++
		pc--
	}
	pn := n
	d1 := min(pa, pb-pa)
	swapN(a, 0, pb-d1, d1)
	d1 = min(pd-pc, pn-pd-1)
	swapN(a, pb, pn-d1, d1)
	d1 = pb - pa
	d2 := pd - pc
	if d1 <= d2 {
		// Recurse on left partition, then iterate on right partition.
		if d1 > 1 {
			pgQsort(a[:d1])
		}
		if d2 > 1 {
			a = a[pn-d2:]
			n = d2
			goto loop
		}
	} else {
		// Recurse on right partition, then iterate on left partition.
		if d2 > 1 {
			pgQsort(a[pn-d2 : pn])
		}
		if d1 > 1 {
			a = a[:d1]
			n = d1
			goto loop
		}
	}
}
