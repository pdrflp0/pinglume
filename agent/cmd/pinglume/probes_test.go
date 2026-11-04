package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"pinglume/agent/internal/endpoints"
	"pinglume/agent/internal/store"
)

func testCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "teste"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func baseConfig() probeConfig {
	return probeConfig{tcpInterval: 10 * time.Second, tcpTimeout: 3 * time.Second,
		httpsInterval: time.Minute, httpsTimeout: 5 * time.Second,
		dnsInterval: 30 * time.Second, dnsTimeout: 3 * time.Second,
		udpInterval: 15 * time.Second, udpWait: time.Second}
}

func TestBuildJobsFromEndpoints(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cert := testCertPEM(t)
	cfg := baseConfig()
	cfg.owner = "pinglume"
	cfg.endpoints = []endpoints.Endpoint{
		{Label: "sp-1", Region: "sa-saopaulo-1", IP: "203.0.113.10", TCPPort: 443, UDPPort: 443, ServerName: "sp-1.pinglume.invalid", CertPEM: cert},
		{Label: "us-1", Region: "us-east1", IP: "203.0.113.11", TCPPort: 443, ServerName: "us-1.pinglume.invalid", CertPEM: cert, DNSZone: "p.exemplo.org"},
	}
	jobs, err := buildJobs(ctx, st, cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, j := range jobs {
		got = append(got, j.Def.Kind+" "+j.Def.Target)
	}
	sort.Strings(got)
	want := []string{
		"dns <nonce>.p.exemplo.org",
		"https https://203.0.113.10:443/p",
		"https https://203.0.113.11:443/p",
		"tcp 203.0.113.10:443",
		"tcp 203.0.113.11:443",
		"udp_echo 203.0.113.10:443",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v", got)
	}
}

func TestLoadEndpoints(t *testing.T) {
	cert := testCertPEM(t)
	file := filepath.Join(t.TempDir(), "e.json")
	write := func(ip string) {
		t.Helper()
		b, _ := json.Marshal([]endpoints.Endpoint{{Label: "a", Region: "r", IP: ip, TCPPort: 443, ServerName: "x", CertPEM: cert}})
		if err := os.WriteFile(file, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("127.0.0.1")
	var c probeConfig
	if err := c.loadEndpoints(file); err != nil {
		t.Fatal(err)
	}
	if len(c.endpoints) != 1 || c.owner != "dev" {
		t.Fatalf("%+v", c)
	}

	write("203.0.113.10")
	c = probeConfig{}
	if err := c.loadEndpoints(file); err == nil || !strings.Contains(err.Error(), "só aceita loopback") {
		t.Fatalf("erro = %v", err)
	}

	c = probeConfig{}
	c.targets = devTargets{{Label: "a"}}
	if err := c.loadEndpoints(""); err != nil || len(c.endpoints) != 0 {
		t.Fatalf("alvos de desenvolvimento não deveriam carregar a lista embutida: %v %v", err, c.endpoints)
	}
}
