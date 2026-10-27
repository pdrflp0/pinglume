package netctx

import (
	"bufio"
	"context"
	"strings"
)

const (
	KindWiFi     = "wifi"
	KindEthernet = "ethernet"
	KindVPN      = "vpn"
	KindOther    = "other"
	KindUnknown  = "unknown"
)

type Context struct {
	Interface string `json:"interface"`
	Kind      string `json:"kind"`
	Method    string `json:"method"`
}

func (c Context) Equal(o Context) bool { return c.Interface == o.Interface && c.Kind == o.Kind }

type Detector interface {
	Detect(ctx context.Context) Context
}

func parseHardwarePorts(out string) map[string]string {
	ports := map[string]string{}
	var port string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "Hardware Port:"); ok {
			port = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(line, "Device:"); ok && port != "" {
			ports[strings.TrimSpace(v)] = port
			port = ""
		}
	}
	return ports
}

func classify(iface, port string) string {
	switch {
	case iface == "":
		return KindUnknown
	case strings.HasPrefix(iface, "utun"), strings.HasPrefix(iface, "ipsec"),
		strings.HasPrefix(iface, "ppp"), strings.HasPrefix(iface, "tun"), strings.HasPrefix(iface, "wg"):
		return KindVPN
	}
	p := strings.ToLower(port)
	switch {
	case p == "":
		return KindUnknown
	case strings.Contains(p, "wi-fi"), strings.Contains(p, "airport"), strings.Contains(p, "wlan"):
		return KindWiFi
	case strings.Contains(p, "ethernet"), strings.Contains(p, "lan"), strings.Contains(p, "thunderbolt bridge"):
		return KindEthernet
	}
	return KindOther
}
