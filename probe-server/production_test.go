package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnLimiterPerIPAndRelease(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	l := newConnLimiter(ln, 10, 2)

	accepted := make(chan net.Conn, 8)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()

	dial := func() net.Conn {
		t.Helper()
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	take := func() net.Conn {
		t.Helper()
		select {
		case c := <-accepted:
			return c
		case <-time.After(2 * time.Second):
			t.Fatal("conexão não aceita")
			return nil
		}
	}

	dial()
	first := take()
	dial()
	take()

	refused := dial()
	refused.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = refused.Read(make([]byte, 1))
	var ne net.Error
	if err == nil || errors.As(err, &ne) && ne.Timeout() {
		t.Fatalf("a terceira conexão do IP deveria ser fechada, veio %v", err)
	}

	first.Close()
	first.Close()
	dial()
	take()
}

func TestConnLimiterTotal(t *testing.T) {
	l := &connLimiter{maxTotal: 2, maxPerIP: 2, perIP: map[string]int{}}
	if !l.acquire("a") || !l.acquire("b") || l.acquire("c") {
		t.Fatal("limite total não respeitado")
	}
	l.release("a")
	if !l.acquire("c") {
		t.Fatal("a vaga deveria ter sido liberada")
	}
	if len(l.perIP) != 2 {
		t.Fatalf("perIP = %v", l.perIP)
	}
}

func TestUDPEchoGlobalLimit(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go serveUDPEcho(pc, &Faults{f: defaultFaults()}, 3)

	c, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for range 10 {
		if _, err := c.Write([]byte("PGLU-teste")); err != nil {
			t.Fatal(err)
		}
	}
	got := 0
	buf := make([]byte, 64)
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		if _, err := c.Read(buf); err != nil {
			break
		}
		got++
	}
	if got != 3 {
		t.Fatalf("ecos = %d, queria 3", got)
	}
}

func TestEndpointEntry(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := loadOrCreateCert(dir, "sp-1.pinglume.invalid", 10); err != nil {
		t.Fatal(err)
	}
	line, err := endpointEntry(dir, "sp-1", "sa-saopaulo-1", "203.0.113.10", "sp-1.pinglume.invalid", "0.0.0.0:443", "0.0.0.0:443")
	if err != nil {
		t.Fatal(err)
	}
	var e endpointJSON
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatal(err)
	}
	cert, _ := os.ReadFile(filepath.Join(dir, "probe-server.pem"))
	if e.IP != "203.0.113.10" || e.TCPPort != 443 || e.UDPPort != 443 || e.CertPEM != string(cert) {
		t.Fatalf("%+v", e)
	}

	line, err = endpointEntry(dir, "us-1", "us-east1", "203.0.113.11", "x", "0.0.0.0:443", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "udp_port") {
		t.Fatal("sem UDP o campo deveria sumir: " + line)
	}
	if _, err := endpointEntry(dir, "sp-1", "r", "nome", "x", "0.0.0.0:443", ""); err == nil {
		t.Fatal("IP inválido deveria falhar")
	}
	if _, err := endpointEntry(dir, "sp-1", "", "203.0.113.10", "x", "0.0.0.0:443", ""); err == nil {
		t.Fatal("região vazia deveria falhar")
	}
}
