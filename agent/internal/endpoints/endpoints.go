package endpoints

import (
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/netip"
)

type Scope int

const (
	Public Scope = iota
	Local
)

type Endpoint struct {
	Label      string `json:"label"`
	Region     string `json:"region"`
	IP         string `json:"ip"`
	TCPPort    uint16 `json:"tcp_port"`
	UDPPort    uint16 `json:"udp_port,omitempty"`
	ServerName string `json:"server_name"`
	CertPEM    string `json:"cert_pem"`
	DNSZone    string `json:"dns_zone,omitempty"`
}

//go:embed production.json
var embedded []byte

func Production() ([]Endpoint, error) {
	list, err := Parse(embedded, Public)
	if err != nil {
		return nil, fmt.Errorf("lista embutida: %w", err)
	}
	return list, nil
}

func Parse(b []byte, scope Scope) ([]Endpoint, error) {
	var list []Endpoint
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, e := range list {
		if err := e.validate(scope); err != nil {
			return nil, fmt.Errorf("endpoint %q: %w", e.Label, err)
		}
		if seen[e.Label] {
			return nil, fmt.Errorf("endpoint %q repetido", e.Label)
		}
		seen[e.Label] = true
	}
	return list, nil
}

func (e Endpoint) validate(scope Scope) error {
	if e.Label == "" || e.ServerName == "" || e.TCPPort == 0 {
		return errors.New("label, server_name e tcp_port são obrigatórios")
	}
	addr, err := netip.ParseAddr(e.IP)
	if err != nil {
		return fmt.Errorf("ip: %w", err)
	}
	if local := isLocal(addr); local != (scope == Local) {
		if local {
			return fmt.Errorf("ip %s recusado: a lista de produção só aceita endereços públicos", addr)
		}
		return fmt.Errorf("ip %s recusado: a lista de desenvolvimento só aceita loopback ou rede privada", addr)
	}
	if _, _, err := ParsePin([]byte(e.CertPEM)); err != nil {
		return fmt.Errorf("cert_pem: %w", err)
	}
	return nil
}

func isLocal(a netip.Addr) bool {
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast()
}

func ParsePin(b []byte) (*x509.CertPool, string, error) {
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, "", errors.New("não contém um certificado PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, "", err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	sum := sha256.Sum256(cert.Raw)
	return pool, hex.EncodeToString(sum[:8]), nil
}

func (e Endpoint) TCPAddr() string {
	return netip.AddrPortFrom(netip.MustParseAddr(e.IP), e.TCPPort).String()
}

func (e Endpoint) UDPAddr() string {
	return netip.AddrPortFrom(netip.MustParseAddr(e.IP), e.UDPPort).String()
}
