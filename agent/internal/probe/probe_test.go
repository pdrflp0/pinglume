package probe

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPConnectOK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	r := TCPConnect{Addr: ln.Addr().String(), Timeout: time.Second}.Probe(context.Background())
	if r.Status != "ok" || r.Duration <= 0 {
		t.Fatalf("esperava ok com duração > 0, veio %+v", r)
	}
}

func TestTCPConnectRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // porta agora fechada
	r := TCPConnect{Addr: addr, Timeout: time.Second}.Probe(context.Background())
	if r.Status != "refused" {
		t.Fatalf("esperava refused, veio %+v", r)
	}
}

// Cancelamento pelo agente (pausa, Ctrl+C) não pode ser contado como timeout de rede.
func TestCancelIsNotTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := TCPConnect{Addr: "127.0.0.1:9", Timeout: time.Second}.Probe(ctx)
	if r.Status != "cancelled" {
		t.Fatalf("esperava cancelled, veio %+v", r)
	}
}

func TestClassifyDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if got := Classify(ctx, context.DeadlineExceeded); got != "timeout" {
		t.Fatalf("esperava timeout, veio %s", got)
	}
}
