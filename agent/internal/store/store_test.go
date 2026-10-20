package store

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestMigrationIsIdempotentAndPreservesData(t *testing.T) {
	ctx := context.Background()
	s, path := openTemp(t)
	now := time.Now()
	dev, err := s.EnsureDevice(ctx, Device{Name: "mac", OS: "darwin", Arch: "arm64", AgentVersion: "t"}, now)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	v, _ := s2.SchemaVersion(ctx)
	files, _ := fs.Glob(migrationFS, "migrations/*.sql")
	if v != len(files) {
		t.Fatalf("versão do schema = %d", v)
	}
	again, err := s2.EnsureDevice(ctx, Device{Name: "outro", AgentVersion: "t2"}, now)
	if err != nil || again.ID != dev.ID {
		t.Fatalf("dispositivo deveria persistir: %+v %v", again, err)
	}
}

func TestProbeDefinitionVersioning(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	now := time.Now()
	ep := Endpoint{Label: "ep-1", Host: "127.0.0.1", Owner: "dev"}
	base := ProbeDefinition{Kind: "tcp", Target: "127.0.0.1:9443", Interval: 10 * time.Second, Timeout: 3 * time.Second, ProtocolVersion: 1}

	a, _ := s.EnsureProbeDefinition(ctx, ep, base, now)
	b, _ := s.EnsureProbeDefinition(ctx, ep, base, now)
	if a.ID != b.ID {
		t.Fatal("definição idêntica deveria ser reutilizada")
	}
	changed := base
	changed.Timeout = 2 * time.Second
	c, _ := s.EnsureProbeDefinition(ctx, ep, changed, now)
	if c.ID == a.ID {
		t.Fatal("parâmetro diferente deveria criar nova versão da definição")
	}
}

func TestSeqContinuesAndIsUnique(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	now := time.Now()
	dev, _ := s.EnsureDevice(ctx, Device{Name: "mac", AgentVersion: "t"}, now)
	pd, _ := s.EnsureProbeDefinition(ctx, Endpoint{Label: "ep", Host: "h", Owner: "dev"},
		ProbeDefinition{Kind: "tcp", Target: "h:1", Interval: time.Second, Timeout: time.Millisecond, ProtocolVersion: 1}, now)

	obs := Observation{ID: "01A", DeviceID: dev.ID, ProbeDefID: pd.ID, Seq: 1, StartedWall: now, Status: "ok", ReceivedAt: now}
	if err := s.InsertObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	obs.ID = "01B"
	if err := s.InsertObservation(ctx, obs); err == nil {
		t.Fatal("seq duplicada para a mesma sonda deveria falhar")
	}
	next, _ := s.NextSeq(ctx, pd.ID)
	if next != 2 {
		t.Fatalf("próxima seq = %d", next)
	}
}

func TestStatusConstraintRejectsUnknown(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	now := time.Now()
	dev, _ := s.EnsureDevice(ctx, Device{Name: "mac", AgentVersion: "t"}, now)
	pd, _ := s.EnsureProbeDefinition(ctx, Endpoint{Label: "ep", Host: "h", Owner: "dev"},
		ProbeDefinition{Kind: "tcp", Target: "h:1", Interval: time.Second, Timeout: time.Millisecond, ProtocolVersion: 1}, now)
	err := s.InsertObservation(ctx, Observation{ID: "01C", DeviceID: dev.ID, ProbeDefID: pd.ID, Seq: 1,
		StartedWall: now, Status: "packet_loss", ReceivedAt: now})
	if err == nil {
		t.Fatal("status fora do vocabulário (ex.: 'packet_loss') deveria ser rejeitado")
	}
}
