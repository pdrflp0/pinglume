package stats

import "testing"

func TestQuantileMatchesUnweighted(t *testing.T) {
	xs := []Weighted{{1, 1}, {2, 1}, {3, 1}, {4, 1}, {5, 1}}
	if *Quantile(xs, 0.5) != 3 || *Quantile(xs, 0.95) != 5 {
		t.Fatal("quantis sem peso")
	}
	if *Quantile([]Weighted{{10, 9}, {100, 1}}, 0.5) != 10 {
		t.Fatal("peso não respeitado")
	}
	if Quantile(nil, 0.5) != nil || Quantile([]Weighted{{1, 0}}, 0.5) != nil {
		t.Fatal("sem peso não tem quantil")
	}
}
