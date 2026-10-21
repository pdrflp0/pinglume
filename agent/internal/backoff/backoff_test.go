package backoff

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/store"
)

func TestSpans(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	ev := func(at time.Duration, kind, id string, m int) {
		st.InsertSystemEvent(ctx, t0.Add(at), kind, `{"probeDefId":"`+id+`","multiplier":`+string(rune('0'+m))+`}`)
	}
	ev(-10*time.Minute, "probe_backoff", "a", 2) // começou antes do período
	ev(-5*time.Minute, "probe_backoff", "a", 4)
	ev(2*time.Minute, "probe_backoff_end", "a", 1)
	ev(5*time.Minute, "probe_backoff", "b", 2) // sem fim: vai até "to"
	ev(6*time.Minute, "probe_backoff_end", "c", 1)

	spans, err := Spans(ctx, st, t0, t0.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a := spans["a"]
	if len(a) != 1 || !a[0].Start.Equal(t0) || a[0].End.Sub(a[0].Start) != 2*time.Minute || a[0].MaxMultiplier != 4 {
		t.Fatalf("a: %+v", a)
	}
	b := spans["b"]
	if len(b) != 1 || Total(b) != 5*time.Minute {
		t.Fatalf("b: %+v", b)
	}
	if len(spans["c"]) != 0 {
		t.Fatal("fim sem início não gera período")
	}
}
