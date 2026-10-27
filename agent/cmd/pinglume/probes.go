package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

type probeConfig struct {
	targets, resolvers          devTargets
	tcpInterval, tcpTimeout     time.Duration
	httpsInterval, httpsTimeout time.Duration
	dnsInterval, dnsTimeout     time.Duration
	devCA, serverName, zone     string
}

func (c probeConfig) validate() error {
	for _, p := range []struct {
		name              string
		interval, timeout time.Duration
	}{{"tcp", c.tcpInterval, c.tcpTimeout}, {"https", c.httpsInterval, c.httpsTimeout}, {"dns", c.dnsInterval, c.dnsTimeout}} {
		if p.timeout <= 0 || p.timeout >= p.interval {
			return fmt.Errorf("%s-timeout precisa ser positivo e menor que %s-interval", p.name, p.name)
		}
	}
	return nil
}

func loadPinnedCA(path string) (*x509.CertPool, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, "", errors.New("arquivo não contém um certificado PEM")
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

func params(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func buildJobs(ctx context.Context, st *store.Store, c probeConfig, now time.Time) ([]sched.Job, error) {
	var pool *x509.CertPool
	var pin string
	if c.devCA != "" {
		var err error
		if pool, pin, err = loadPinnedCA(c.devCA); err != nil {
			return nil, fmt.Errorf("--dev-ca: %w", err)
		}
	}

	var jobs []sched.Job
	add := func(label, host string, p probe.Prober, target string, interval, timeout time.Duration, paramsJSON string) error {
		def, err := st.EnsureProbeDefinition(ctx,
			store.Endpoint{Label: label, Host: host, Owner: "dev"},
			store.ProbeDefinition{Kind: p.Kind(), Target: target, Interval: interval, Timeout: timeout,
				ProtocolVersion: p.ProtocolVersion(), ParamsJSON: paramsJSON}, now)
		if err != nil {
			return err
		}
		jobs = append(jobs, sched.Job{Def: def, Prober: p})
		return nil
	}

	for _, t := range c.targets {
		addr, host := t.Addr.String(), t.Addr.Addr().String()
		if err := add(t.Label, host, probe.TCPConnect{Addr: addr, Timeout: c.tcpTimeout},
			addr, c.tcpInterval, c.tcpTimeout, ""); err != nil {
			return nil, err
		}
		if pool == nil {
			continue
		}
		url := "https://" + addr + "/p"
		if err := add(t.Label, host,
			probe.HTTPSGet{URL: url, ServerName: c.serverName, RootCAs: pool, Timeout: c.httpsTimeout},
			url, c.httpsInterval, c.httpsTimeout,
			params(map[string]string{"serverName": c.serverName, "rootCA": "pinned:" + pin})); err != nil {
			return nil, err
		}
	}
	for _, r := range c.resolvers {
		addr := r.Addr.String()
		if err := add(r.Label, r.Addr.Addr().String(),
			probe.DNSResolve{Zone: c.zone, Resolver: addr, Timeout: c.dnsTimeout},
			"<nonce>."+c.zone, c.dnsInterval, c.dnsTimeout,
			params(map[string]string{"resolver": addr, "zone": c.zone})); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}
