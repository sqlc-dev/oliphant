// pg_qsort (src/port/qsort.c via lib/sort_template.h): the Bentley & McIlroy
// quicksort PostgreSQL uses — list_sort() maps to it at the pin (port.h
// defines qsort as pg_qsort). It is not stable, so the order of
// equal-priority truncations depends on the exact algorithm; byte-parity
// needs the algorithm, not just the comparator. Same port as
// internal/normalize/qsort.go, over possible truncations.
package summary

func med3T(a []*possibleTruncation, x, y, z int) int {
	if cmpPossibleTruncations(a[x], a[y]) < 0 {
		if cmpPossibleTruncations(a[y], a[z]) < 0 {
			return y
		}
		if cmpPossibleTruncations(a[x], a[z]) < 0 {
			return z
		}
		return x
	}
	if cmpPossibleTruncations(a[y], a[z]) > 0 {
		return y
	}
	if cmpPossibleTruncations(a[x], a[z]) < 0 {
		return x
	}
	return z
}

func swapNT(a []*possibleTruncation, x, y, n int) {
	for i := 0; i < n; i++ {
		a[x+i], a[y+i] = a[y+i], a[x+i]
	}
}

// pgQsortTruncations sorts a exactly as ST_SORT in sort_template.h.
func pgQsortTruncations(a []*possibleTruncation) {
	n := len(a)
loop:
	if n < 7 {
		for pm := 1; pm < n; pm++ {
			for pl := pm; pl > 0 && cmpPossibleTruncations(a[pl-1], a[pl]) > 0; pl-- {
				a[pl], a[pl-1] = a[pl-1], a[pl]
			}
		}
		return
	}
	presorted := true
	for pm := 1; pm < n; pm++ {
		if cmpPossibleTruncations(a[pm-1], a[pm]) > 0 {
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
			pl = med3T(a, pl, pl+d, pl+2*d)
			pm = med3T(a, pm-d, pm, pm+d)
			pn = med3T(a, pn-2*d, pn-d, pn)
		}
		pm = med3T(a, pl, pm, pn)
	}
	a[0], a[pm] = a[pm], a[0]
	pa, pb := 1, 1
	pc, pd := n-1, n-1
	for {
		var r int
		for pb <= pc {
			if r = cmpPossibleTruncations(a[pb], a[0]); r > 0 {
				break
			}
			if r == 0 {
				a[pa], a[pb] = a[pb], a[pa]
				pa++
			}
			pb++
		}
		for pb <= pc {
			if r = cmpPossibleTruncations(a[pc], a[0]); r < 0 {
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
	swapNT(a, 0, pb-d1, d1)
	d1 = min(pd-pc, pn-pd-1)
	swapNT(a, pb, pn-d1, d1)
	d1 = pb - pa
	d2 := pd - pc
	if d1 <= d2 {
		// Recurse on left partition, then iterate on right partition.
		if d1 > 1 {
			pgQsortTruncations(a[:d1])
		}
		if d2 > 1 {
			a = a[pn-d2:]
			n = d2
			goto loop
		}
	} else {
		// Recurse on right partition, then iterate on left partition.
		if d2 > 1 {
			pgQsortTruncations(a[pn-d2 : pn])
		}
		if d1 > 1 {
			a = a[:d1]
			n = d1
			goto loop
		}
	}
}
