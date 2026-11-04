package endpoints

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testCert(t *testing.T) string {
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

func encode(t *testing.T, list ...Endpoint) []byte {
	t.Helper()
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse(t *testing.T) {
	cert := testCert(t)
	public := Endpoint{Label: "sp-1", Region: "sa-saopaulo-1", IP: "203.0.113.10", TCPPort: 443, UDPPort: 443, ServerName: "sp-1.pinglume.invalid", CertPEM: cert}
	local := public
	local.IP = "127.0.0.1"
	private := public
	private.IP = "10.0.0.8"
	noCert := public
	noCert.CertPEM = "x"
	noPort := public
	noPort.TCPPort = 0
	badIP := public
	badIP.IP = "exemplo.com"

	cases := []struct {
		name    string
		scope   Scope
		list    []Endpoint
		wantErr string
	}{
		{"público em produção", Public, []Endpoint{public}, ""},
		{"loopback em desenvolvimento", Local, []Endpoint{local}, ""},
		{"privado em desenvolvimento", Local, []Endpoint{private}, ""},
		{"loopback em produção", Public, []Endpoint{local}, "só aceita endereços públicos"},
		{"privado em produção", Public, []Endpoint{private}, "só aceita endereços públicos"},
		{"público em desenvolvimento", Local, []Endpoint{public}, "só aceita loopback"},
		{"sem certificado", Public, []Endpoint{noCert}, "cert_pem"},
		{"sem porta", Public, []Endpoint{noPort}, "obrigatórios"},
		{"nome em vez de IP", Public, []Endpoint{badIP}, "ip:"},
		{"rótulo repetido", Public, []Endpoint{public, public}, "repetido"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(encode(t, c.list...), c.scope)
			if c.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != len(c.list) {
					t.Fatalf("len = %d", len(got))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("erro = %v, queria %q", err, c.wantErr)
			}
		})
	}
}

func TestProductionListIsValid(t *testing.T) {
	if _, err := Production(); err != nil {
		t.Fatal(err)
	}
}

func TestAddrs(t *testing.T) {
	e := Endpoint{IP: "203.0.113.10", TCPPort: 443, UDPPort: 8443}
	if e.TCPAddr() != "203.0.113.10:443" || e.UDPAddr() != "203.0.113.10:8443" {
		t.Fatal(e.TCPAddr(), e.UDPAddr())
	}
}
