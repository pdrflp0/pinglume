package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"pinglume/agent/internal/endpoints"
	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

type probeConfig struct {
	targets, resolvers, udps    devTargets
	udpInterval, udpWait        time.Duration
	tcpInterval, tcpTimeout     time.Duration
	httpsInterval, httpsTimeout time.Duration
	dnsInterval, dnsTimeout     time.Duration
	devCA, serverName, zone     string
	endpoints                   []endpoints.Endpoint
	owner                       string
}

func (c probeConfig) validate() error {
	for _, p := range []struct {
		name              string
		interval, timeout time.Duration
	}{{"tcp", c.tcpInterval, c.tcpTimeout}, {"https", c.httpsInterval, c.httpsTimeout}, {"dns", c.dnsInterval, c.dnsTimeout},
		{"udp", c.udpInterval, udpBurstCount*udpBurstSpacing + c.udpWait}} {
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
	return endpoints.ParsePin(b)
}

const (
	udpBurstCount   = 20
	udpBurstSpacing = 20 * time.Millisecond
)

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
	add := func(ep store.Endpoint, p probe.Prober, target string, interval, timeout time.Duration, paramsJSON string) error {
		def, err := st.EnsureProbeDefinition(ctx, ep,
			store.ProbeDefinition{Kind: p.Kind(), Target: target, Interval: interval, Timeout: timeout,
				ProtocolVersion: p.ProtocolVersion(), ParamsJSON: paramsJSON}, now)
		if err != nil {
			return err
		}
		jobs = append(jobs, sched.Job{Def: def, Prober: p})
		return nil
	}

	dev := func(label, host string) store.Endpoint { return store.Endpoint{Label: label, Host: host, Owner: "dev"} }

	for _, t := range c.targets {
		addr, ep := t.Addr.String(), dev(t.Label, t.Addr.Addr().String())
		if err := add(ep, probe.TCPConnect{Addr: addr, Timeout: c.tcpTimeout},
			addr, c.tcpInterval, c.tcpTimeout, ""); err != nil {
			return nil, err
		}
		if pool == nil {
			continue
		}
		url := "https://" + addr + "/p"
		if err := add(ep,
			probe.HTTPSGet{URL: url, ServerName: c.serverName, RootCAs: pool, Timeout: c.httpsTimeout},
			url, c.httpsInterval, c.httpsTimeout,
			params(map[string]string{"serverName": c.serverName, "rootCA": "pinned:" + pin})); err != nil {
			return nil, err
		}
	}
	for _, r := range c.resolvers {
		addr := r.Addr.String()
		if err := add(dev(r.Label, r.Addr.Addr().String()),
			probe.DNSResolve{Zone: c.zone, Resolver: addr, Timeout: c.dnsTimeout},
			"<nonce>."+c.zone, c.dnsInterval, c.dnsTimeout,
			params(map[string]string{"resolver": addr, "zone": c.zone})); err != nil {
			return nil, err
		}
	}
	for _, u := range c.udps {
		addr := u.Addr.String()
		if err := add(dev(u.Label, u.Addr.Addr().String()),
			probe.UDPEcho{Addr: addr, Count: udpBurstCount, Spacing: udpBurstSpacing, Wait: c.udpWait},
			addr, c.udpInterval, udpBurstCount*udpBurstSpacing+c.udpWait,
			params(map[string]any{"count": udpBurstCount, "spacingMs": udpBurstSpacing.Milliseconds(), "bytes": 64})); err != nil {
			return nil, err
		}
	}
	for _, e := range c.endpoints {
		ep := store.Endpoint{Label: e.Label, Host: e.IP, Region: e.Region, Owner: c.owner}
		epPool, epPin, err := endpoints.ParsePin([]byte(e.CertPEM))
		if err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", e.Label, err)
		}
		addr := e.TCPAddr()
		if err := add(ep, probe.TCPConnect{Addr: addr, Timeout: c.tcpTimeout},
			addr, c.tcpInterval, c.tcpTimeout, ""); err != nil {
			return nil, err
		}
		url := "https://" + addr + "/p"
		if err := add(ep,
			probe.HTTPSGet{URL: url, ServerName: e.ServerName, RootCAs: epPool, Timeout: c.httpsTimeout},
			url, c.httpsInterval, c.httpsTimeout,
			params(map[string]string{"serverName": e.ServerName, "rootCA": "pinned:" + epPin})); err != nil {
			return nil, err
		}
		if e.DNSZone != "" {
			if err := add(ep, probe.DNSResolve{Zone: e.DNSZone, Timeout: c.dnsTimeout},
				"<nonce>."+e.DNSZone, c.dnsInterval, c.dnsTimeout,
				params(map[string]string{"resolver": "sistema", "zone": e.DNSZone})); err != nil {
				return nil, err
			}
		}
		if e.UDPPort != 0 {
			udp := e.UDPAddr()
			if err := add(ep,
				probe.UDPEcho{Addr: udp, Count: udpBurstCount, Spacing: udpBurstSpacing, Wait: c.udpWait},
				udp, c.udpInterval, udpBurstCount*udpBurstSpacing+c.udpWait,
				params(map[string]any{"count": udpBurstCount, "spacingMs": udpBurstSpacing.Milliseconds(), "bytes": 64})); err != nil {
				return nil, err
			}
		}
	}
	return jobs, nil
}
