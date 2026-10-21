package probe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"time"
)

// DNSResolve mede o tempo de resolver um nome único dentro da zona Pinglume.
// O nome muda a cada execução (<nonce>.<zona>) para que nenhum cache responda
// no lugar do servidor autoritativo.
//
// Resolver vazio usa o resolvedor do sistema — é o que interessa em produção,
// porque é o caminho que os aplicativos da pessoa usam. Um endereço explícito
// (ip:porta) só serve para o testbed local, e a definição da sonda registra
// isso para que as duas medidas nunca sejam comparadas como se fossem iguais.
type DNSResolve struct {
	Zone     string
	Resolver string
	Timeout  time.Duration
}

func (DNSResolve) Kind() string         { return "dns" }
func (DNSResolve) ProtocolVersion() int { return 1 }

func (p DNSResolve) resolver() *net.Resolver {
	if p.Resolver == "" {
		return net.DefaultResolver
	}
	addr := p.Resolver
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", addr)
	}}
}

func (p DNSResolve) Probe(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	name := nonce() + "." + p.Zone
	start := time.Now()
	addrs, err := p.resolver().LookupNetIP(ctx, "ip4", name)
	elapsed := time.Since(start)
	if err != nil {
		return Result{Duration: elapsed, Status: classifyDNS(ctx, err), ErrorDetail: err.Error()}
	}
	if len(addrs) == 0 {
		return Result{Duration: elapsed, Status: "dns_error", ErrorDetail: "resposta sem endereço IPv4"}
	}
	return Result{Duration: elapsed, Status: "ok"}
}

func classifyDNS(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	var de *net.DNSError
	if errors.As(err, &de) {
		if de.IsTimeout || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "timeout"
		}
		return "dns_error" // NXDOMAIN, SERVFAIL, REFUSED…: o detalhe guarda qual
	}
	return Classify(ctx, err)
}

func nonce() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
