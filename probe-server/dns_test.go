package main

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func startDNS(t *testing.T, fault string) (string, *Faults) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	f := &Faults{f: defaultFaults()}
	s := f.Get()
	s.DNS = fault
	if err := f.Set(s); err != nil {
		t.Fatal(err)
	}
	go serveDNS(pc, "p.pinglume.test", netip.MustParseAddr("127.0.0.1"), f)
	return pc.LocalAddr().String(), f
}

func resolver(addr string) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", addr)
	}}
}

func TestAnswersAInZone(t *testing.T) {
	addr, _ := startDNS(t, "ok")
	ips, err := resolver(addr).LookupNetIP(context.Background(), "ip4", "abc123.p.pinglume.test")
	if err != nil || len(ips) != 1 || ips[0].String() != "127.0.0.1" {
		t.Fatalf("%v %v", ips, err)
	}

	if _, err := resolver(addr).LookupNetIP(context.Background(), "ip4", "X.P.Pinglume.Test"); err != nil {
		t.Fatal(err)
	}
}

func TestRefusesOutsideZone(t *testing.T) {
	addr, _ := startDNS(t, "ok")
	_, err := resolver(addr).LookupNetIP(context.Background(), "ip4", "example.com")
	if err == nil {
		t.Fatal("fora da zona não deveria resolver")
	}
}

func TestFaults(t *testing.T) {
	cases := map[string]func(*net.DNSError) bool{
		"nxdomain": func(e *net.DNSError) bool { return e.IsNotFound },
		"servfail": func(e *net.DNSError) bool { return !e.IsNotFound && !e.IsTimeout },
		"drop":     func(e *net.DNSError) bool { return e.IsTimeout },
	}
	for fault, check := range cases {
		addr, _ := startDNS(t, fault)
		ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
		_, err := resolver(addr).LookupNetIP(ctx, "ip4", "n.p.pinglume.test")
		cancel()
		var de *net.DNSError
		if !asDNSError(err, &de) || !check(de) {
			t.Errorf("%s: erro inesperado %v", fault, err)
		}
	}
}

func asDNSError(err error, out **net.DNSError) bool {
	de, ok := err.(*net.DNSError)
	if ok {
		*out = de
	}
	return ok
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, make([]byte, 11), []byte(strings.Repeat("\xff", 40))} {
		if _, err := parseQuery(b); err == nil {
			t.Errorf("deveria rejeitar %x", b)
		}
	}
}

func TestFaultValidation(t *testing.T) {
	f := &Faults{f: defaultFaults()}
	bad := []FaultState{
		{DNS: "talvez", HTTPS: "ok", HTTPSDelay: "0s", Listener: "open"},
		{DNS: "ok", HTTPS: "ok", HTTPSDelay: "2h", Listener: "open"},
		{DNS: "ok", HTTPS: "ok", HTTPSDelay: "0s", Listener: "meio"},
	}
	for _, s := range bad {
		if f.Set(s) == nil {
			t.Errorf("deveria rejeitar %+v", s)
		}
	}
}
