package netctx

import (
	"context"
	"runtime"
	"testing"
)

const sample = `
Hardware Port: Ethernet Adapter (en3)
Device: en3
Ethernet Address: 92:94:43:ae:1e:2e

Hardware Port: Wi-Fi
Device: en0
Ethernet Address: 14:7f:ce:b7:38:e6

Hardware Port: Thunderbolt Bridge
Device: bridge0
Ethernet Address: 36:89:96:25:ac:00

VLAN Configurations
===================
`

func TestParseAndClassify(t *testing.T) {
	ports := parseHardwarePorts(sample)
	if ports["en0"] != "Wi-Fi" || ports["en3"] != "Ethernet Adapter (en3)" {
		t.Fatalf("%v", ports)
	}
	cases := map[[2]string]string{
		{"en0", "Wi-Fi"}:                  KindWiFi,
		{"en3", "Ethernet Adapter (en3)"}: KindEthernet,
		{"en7", "USB 10/100/1000 LAN"}:    KindEthernet,
		{"utun4", ""}:                     KindVPN,
		{"en9", "iPhone USB"}:             KindOther,
		{"en5", ""}:                       KindUnknown,
		{"", ""}:                          KindUnknown,
	}
	for in, want := range cases {
		if got := classify(in[0], in[1]); got != want {
			t.Errorf("classify(%q, %q) = %s, esperava %s", in[0], in[1], got, want)
		}
	}
}

func TestSystemDetectSmoke(t *testing.T) {
	c := (&System{}).Detect(context.Background())
	t.Logf("detectado: %+v", c)
	if runtime.GOOS != "darwin" && c.Kind != KindUnknown {
		t.Fatalf("fora do macOS deveria ser unknown: %+v", c)
	}
	if c.Interface != "" && c.Kind == "" {
		t.Fatalf("interface sem tipo: %+v", c)
	}
}
