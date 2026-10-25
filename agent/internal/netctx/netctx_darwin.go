//go:build darwin

package netctx

import (
	"context"
	"net"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/route"
)

// System detecta a interface pela rota padrão (tabela de roteamento do
// kernel) e o tipo pela lista de portas do `networksetup`, guardada em cache.
type System struct {
	mu      sync.Mutex
	ports   map[string]string
	fetched time.Time
}

const portsTTL = time.Minute

func (s *System) Detect(ctx context.Context) Context {
	iface := defaultRouteInterface()
	if iface == "" {
		return Context{Kind: KindUnknown, Method: "sem rota padrão"}
	}
	port := s.portFor(ctx, iface)
	return Context{Interface: iface, Kind: classify(iface, port), Method: "rota padrão + networksetup"}
}

func (s *System) portFor(ctx context.Context, iface string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ports == nil || time.Since(s.fetched) > portsTTL || s.ports[iface] == "" && time.Since(s.fetched) > 5*time.Second {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", "-listallhardwareports").Output(); err == nil {
			s.ports = parseHardwarePorts(string(out))
		}
		s.fetched = time.Now()
	}
	return s.ports[iface]
}

// defaultRouteInterface lê a tabela de rotas IPv4 e devolve a interface da
// rota padrão (destino 0.0.0.0/0).
func defaultRouteInterface() string {
	rib, err := route.FetchRIB(syscall.AF_INET, route.RIBTypeRoute, 0)
	if err != nil {
		return ""
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return ""
	}
	for _, m := range msgs {
		rm, ok := m.(*route.RouteMessage)
		if !ok || rm.Flags&syscall.RTF_GATEWAY == 0 || rm.Flags&syscall.RTF_UP == 0 || len(rm.Addrs) <= syscall.RTAX_NETMASK {
			continue
		}
		dst, ok := rm.Addrs[syscall.RTAX_DST].(*route.Inet4Addr)
		if !ok || dst.IP != [4]byte{} {
			continue
		}
		if mask, ok := rm.Addrs[syscall.RTAX_NETMASK].(*route.Inet4Addr); ok && mask.IP != [4]byte{} {
			continue
		}
		if ifi, err := net.InterfaceByIndex(rm.Index); err == nil {
			return ifi.Name
		}
	}
	return ""
}
