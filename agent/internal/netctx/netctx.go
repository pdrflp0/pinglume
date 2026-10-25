// Package netctx detecta por qual interface o computador está saindo para a
// internet e de que tipo ela é (Wi-Fi, cabo, VPN). Só lê o estado do sistema:
// não muda rotas nem configurações, e não coleta o nome da rede Wi-Fi (no
// macOS isso exigiria permissão de Localização).
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

// Context é a interface de saída num instante.
type Context struct {
	Interface string `json:"interface"`
	Kind      string `json:"kind"`
	// Method diz como o tipo foi obtido, para a origem ficar explícita.
	Method string `json:"method"`
}

func (c Context) Equal(o Context) bool { return c.Interface == o.Interface && c.Kind == o.Kind }

type Detector interface {
	Detect(ctx context.Context) Context
}

// parseHardwarePorts lê a saída de `networksetup -listallhardwareports` e
// devolve dispositivo → nome da porta (ex.: "en0" → "Wi-Fi").
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

// classify decide o tipo pelo nome da interface e da porta de hardware.
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
