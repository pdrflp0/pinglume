package stats

import "sort"

type Weighted struct {
	Value  int64
	Weight int
}

func Quantile(xs []Weighted, q float64) *int64 {
	total := 0
	for _, x := range xs {
		total += x.Weight
	}
	if total == 0 {
		return nil
	}
	c := append([]Weighted(nil), xs...)
	sort.Slice(c, func(i, j int) bool { return c[i].Value < c[j].Value })
	target := q * float64(total-1)
	acc := 0
	for _, x := range c {
		acc += x.Weight
		if float64(acc-1) >= target {
			v := x.Value
			return &v
		}
	}
	v := c[len(c)-1].Value
	return &v
}
