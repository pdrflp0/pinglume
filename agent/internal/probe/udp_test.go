package probe

import (
	"context"
	"math/rand/v2"
	"net"
	"testing"
	"time"
)

func echoServer(t *testing.T, loss float64, delay time.Duration) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 256)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if rand.Float64() < loss {
				continue
			}
			msg := append([]byte(nil), buf[:n]...)
			if delay > 0 {
				time.AfterFunc(time.Duration(rand.Int64N(int64(delay))), func() { pc.WriteTo(msg, from) })
				continue
			}
			pc.WriteTo(msg, from)
		}
	}()
	return pc.LocalAddr().String()
}

func burst(addr string) UDPEcho {
	return UDPEcho{Addr: addr, Count: 20, Spacing: 2 * time.Millisecond, Wait: 200 * time.Millisecond}
}

func TestUDPEchoClean(t *testing.T) {
	r := burst(echoServer(t, 0, 0)).Probe(context.Background())
	if r.Status != "ok" || r.Phases["sent"] != 20 || r.Phases["received"] != 20 || r.Phases["lost"] != 0 {
		t.Fatalf("%+v", r)
	}
	if r.Phases["rtt_p50_us"] <= 0 || r.Duration.Microseconds() != r.Phases["rtt_p50_us"] {
		t.Fatalf("RTT: %+v", r)
	}
}

func TestUDPEchoLossAndJitter(t *testing.T) {
	r := burst(echoServer(t, 0.5, 0)).Probe(context.Background())
	lost := r.Phases["lost"]
	if r.Status != "ok" || lost < 3 || lost > 17 || r.Phases["sent"]-r.Phases["received"] != lost {
		t.Fatalf("perda de ~50%%: %+v", r.Phases)
	}
	j := burst(echoServer(t, 0, 20*time.Millisecond)).Probe(context.Background())
	if j.Phases["jitter_us"] < 1000 {
		t.Fatalf("atraso aleatório de até 20 ms deveria gerar jitter: %+v", j.Phases)
	}
}

func TestUDPEchoAllLostIsTimeout(t *testing.T) {
	r := burst(echoServer(t, 1, 0)).Probe(context.Background())
	if r.Status != "timeout" || r.Phases["received"] != 0 || r.Phases["sent"] != 20 {
		t.Fatalf("%+v", r)
	}
}

func TestUDPEchoIgnoresForeignReplies(t *testing.T) {
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer pc.Close()
	go func() {
		buf := make([]byte, 256)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			msg := append([]byte(nil), buf[:n]...)
			msg[9] ^= 0xff
			pc.WriteTo(msg, from)
		}
	}()
	if r := burst(pc.LocalAddr().String()).Probe(context.Background()); r.Phases["received"] != 0 {
		t.Fatalf("respostas com outro nonce não podem contar: %+v", r.Phases)
	}
}

func TestUDPEchoCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	r := UDPEcho{Addr: echoServer(t, 0, 0), Count: 20, Spacing: 20 * time.Millisecond, Wait: time.Second}.Probe(ctx)
	if r.Status != "cancelled" {
		t.Fatalf("%+v", r)
	}
}
