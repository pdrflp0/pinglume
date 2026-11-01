package store

import (
	"context"
	"testing"
	"time"
)

func TestModesNeverMix(t *testing.T) {
	ctx := context.Background()

	real, _ := openTemp(t)
	if err := real.ClaimMode(ctx, ModeReal); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	dev, _ := real.EnsureDevice(ctx, Device{Name: "t", AgentVersion: "t"}, now)
	pd, _ := real.EnsureProbeDefinition(ctx, Endpoint{Label: "ep", Host: "h", Owner: "dev"},
		ProbeDefinition{Kind: "tcp", Target: "h:1", Interval: time.Second, Timeout: time.Millisecond, ProtocolVersion: 1}, now)
	real.InsertObservation(ctx, Observation{ID: "o", DeviceID: dev.ID, ProbeDefID: pd.ID, Seq: 1, StartedWall: now, Status: "ok", ReceivedAt: now})
	if err := real.ClaimMode(ctx, ModeDemo); err != ErrRealData {
		t.Fatalf("demo num banco real: %v", err)
	}

	demo, _ := openTemp(t)
	if err := demo.ClaimMode(ctx, ModeDemo); err != nil {
		t.Fatal(err)
	}
	if err := demo.ClaimMode(ctx, ModeReal); err != ErrDemoData {
		t.Fatalf("real num banco de demo: %v", err)
	}
	if m, _ := demo.Mode(ctx); m != ModeDemo {
		t.Fatalf("modo = %s", m)
	}
	if err := demo.ClaimMode(ctx, ModeDemo); err != nil {
		t.Fatal("reabrir a demo deveria funcionar")
	}

	legacy, _ := openTemp(t)
	if m, _ := legacy.Mode(ctx); m != ModeReal {
		t.Fatal("banco sem marca é real")
	}
}
