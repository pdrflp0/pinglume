package timeline

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/store"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func TestBucketsSeparateProbeVersionsAndCountFailures(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, t0)
	ep := store.Endpoint{Label: "ep-1", Host: "127.0.0.1", Owner: "dev"}
	v1, _ := st.EnsureProbeDefinition(ctx, ep, store.ProbeDefinition{Kind: "tcp", Target: "127.0.0.1:1",
		Interval: 10 * time.Second, Timeout: time.Second, ProtocolVersion: 1}, t0)

	v1b, _ := st.EnsureProbeDefinition(ctx, ep, store.ProbeDefinition{Kind: "tcp", Target: "127.0.0.1:1",
		Interval: 10 * time.Second, Timeout: 2 * time.Second, ProtocolVersion: 1}, t0)

	ins := func(def string, seq int64, at time.Time, dur time.Duration, status string) {
		t.Helper()
		err := st.InsertObservation(ctx, store.Observation{ID: fmt.Sprintf("%s-%d", def, seq), DeviceID: dev.ID,
			ProbeDefID: def, Seq: seq, StartedWall: at, Duration: dur, Status: status, ReceivedAt: at})
		if err != nil {
			t.Fatal(err)
		}
	}

	ins(v1.ID, 1, t0.Add(1*time.Second), 1*time.Millisecond, "ok")
	ins(v1.ID, 2, t0.Add(11*time.Second), 2*time.Millisecond, "ok")
	ins(v1.ID, 3, t0.Add(21*time.Second), 9*time.Millisecond, "ok")
	ins(v1.ID, 4, t0.Add(31*time.Second), 500*time.Millisecond, "refused")
	ins(v1.ID, 5, t0.Add(125*time.Second), time.Second, "timeout")
	ins(v1b.ID, 1, t0.Add(5*time.Second), 3*time.Millisecond, "ok")

	tl, err := Build(ctx, st, t0, t0.Add(5*time.Minute), 5, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if tl.BucketMS != 60_000 {
		t.Fatalf("balde = %d ms", tl.BucketMS)
	}
	if len(tl.Series) != 2 {
		t.Fatalf("esperava 2 séries (uma por definição), veio %d", len(tl.Series))
	}
	var s Series
	for _, x := range tl.Series {
		if x.ProbeDefID == v1.ID {
			s = x
		}
	}
	if len(s.Buckets) != 2 {
		t.Fatalf("baldes vazios não devem ser enviados: %+v", s.Buckets)
	}
	b0, b2 := s.Buckets[0], s.Buckets[1]
	if b0.N != 4 || b0.OK != 3 || b0.Fail["refused"] != 1 {
		t.Fatalf("balde 0: %+v", b0)
	}

	if *b0.P50US != 2000 || *b0.P95US != 9000 || *b0.MaxUS != 9000 {
		t.Fatalf("percentis do balde 0: p50=%d p95=%d max=%d", *b0.P50US, *b0.P95US, *b0.MaxUS)
	}
	if b2.T != t0.Add(2*time.Minute).UnixMilli() || b2.P50US != nil || b2.Fail["timeout"] != 1 {
		t.Fatalf("balde 2: %+v", b2)
	}
}
