package probe

import (
	"context"
	"crypto/x509"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDNS responde consultas A da zona com 127.0.0.1, ou se comporta conforme mode:
// "ok", "drop" (não responde) ou "nxdomain".
func fakeDNS(t *testing.T, mode string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if mode == "drop" || n < 12 {
				continue
			}
			q := buf[:n]
			// fim da pergunta: nome terminado em 0, depois tipo e classe
			i := 12
			for i < n && q[i] != 0 {
				i += int(q[i]) + 1
			}
			end := i + 5
			qtype := binary.BigEndian.Uint16(q[i+1 : i+3])
			resp := append([]byte{}, q[:end]...)
			resp[2], resp[3] = 0x84, 0x00 // QR+AA, NOERROR
			if mode == "nxdomain" {
				resp[3] = 0x03
			} else if qtype == 1 {
				resp[7] = 1 // ANCOUNT
				resp = append(resp, 0xC0, 0x0C, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4, 127, 0, 0, 1)
			}
			pc.WriteTo(resp, from)
		}
	}()
	return pc.LocalAddr().String()
}

func TestDNSResolve(t *testing.T) {
	ok := DNSResolve{Zone: "p.pinglume.test", Resolver: fakeDNS(t, "ok"), Timeout: time.Second}.Probe(context.Background())
	if ok.Status != "ok" || ok.Duration <= 0 {
		t.Fatalf("ok: %+v", ok)
	}
	nx := DNSResolve{Zone: "p.pinglume.test", Resolver: fakeDNS(t, "nxdomain"), Timeout: time.Second}.Probe(context.Background())
	if nx.Status != "dns_error" {
		t.Fatalf("nxdomain: %+v", nx)
	}
	drop := DNSResolve{Zone: "p.pinglume.test", Resolver: fakeDNS(t, "drop"), Timeout: 300 * time.Millisecond}.Probe(context.Background())
	if drop.Status != "timeout" {
		t.Fatalf("drop: %+v", drop)
	}
}

func TestDNSNonceIsUnique(t *testing.T) {
	if nonce() == nonce() {
		t.Fatal("nonce repetido")
	}
}

func tlsServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv, pool
}

func TestHTTPSOKWithPhases(t *testing.T) {
	srv, pool := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	r := HTTPSGet{URL: srv.URL + "/p", ServerName: "example.com", RootCAs: pool, Timeout: 2 * time.Second}.Probe(context.Background())
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	for _, k := range []string{"connect", "tls", "ttfb"} {
		if r.Phases[k] <= 0 {
			t.Errorf("fase %s ausente: %v", k, r.Phases)
		}
	}
	if _, hasDNS := r.Phases["dns"]; hasDNS {
		t.Error("URL por IP não deveria ter fase de DNS")
	}
	if !(r.Phases["connect"] <= r.Phases["tls"] && r.Phases["tls"] <= r.Phases["ttfb"]) {
		t.Errorf("fases fora de ordem: %v", r.Phases)
	}
}

func TestHTTPSStatuses(t *testing.T) {
	srv500, pool500 := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) })
	if r := (HTTPSGet{URL: srv500.URL, ServerName: "example.com", RootCAs: pool500, Timeout: time.Second}).Probe(context.Background()); r.Status != "http_error" || !strings.Contains(r.ErrorDetail, "500") {
		t.Errorf("500: %+v", r)
	}

	srv, _ := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {})
	// Raiz errada (sistema) para um certificado de teste: falha de TLS, não de rede.
	if r := (HTTPSGet{URL: srv.URL, ServerName: "example.com", Timeout: time.Second}).Probe(context.Background()); r.Status != "tls_error" {
		t.Errorf("CA desconhecida: %+v", r)
	}

	srvHang, poolHang := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	if r := (HTTPSGet{URL: srvHang.URL, ServerName: "example.com", RootCAs: poolHang, Timeout: 200 * time.Millisecond}).Probe(context.Background()); r.Status != "timeout" {
		t.Errorf("servidor parado: %+v", r)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if r := (HTTPSGet{URL: "https://" + addr + "/p", Timeout: time.Second}).Probe(context.Background()); r.Status != "refused" {
		t.Errorf("porta fechada: %+v", r)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := (HTTPSGet{URL: srv.URL, Timeout: time.Second}).Probe(ctx); r.Status != "cancelled" {
		t.Errorf("cancelada: %+v", r)
	}
}

// Critério de aceite da F5: com o DNS fora, a sonda HTTPS por IP continua
// respondendo, e cada uma é rotulada pelo seu protocolo.
func TestDNSDownHTTPSByIPStillOK(t *testing.T) {
	dns := DNSResolve{Zone: "p.pinglume.test", Resolver: fakeDNS(t, "drop"), Timeout: 300 * time.Millisecond}
	srv, pool := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	https := HTTPSGet{URL: srv.URL + "/p", ServerName: "example.com", RootCAs: pool, Timeout: time.Second}

	d, h := dns.Probe(context.Background()), https.Probe(context.Background())
	if d.Status != "timeout" || h.Status != "ok" {
		t.Fatalf("dns=%s https=%s", d.Status, h.Status)
	}
}
