package probe

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"time"
)

type Result struct {
	Duration    time.Duration
	Status      string
	ErrorDetail string
	Phases      map[string]int64
}

type Prober interface {
	Kind() string
	ProtocolVersion() int
	Probe(ctx context.Context) Result
}

type TCPConnect struct {
	Addr    string
	Timeout time.Duration
}

func (TCPConnect) Kind() string         { return "tcp" }
func (TCPConnect) ProtocolVersion() int { return 1 }

func (p TCPConnect) Probe(ctx context.Context) Result {
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	var d net.Dialer
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", p.Addr)
	elapsed := time.Since(start)
	if err != nil {
		return Result{Duration: elapsed, Status: Classify(ctx, err), ErrorDetail: err.Error()}
	}
	conn.Close()
	return Result{Duration: elapsed, Status: "ok"}
}

func Classify(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETDOWN):
		return "net_unreachable"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns_error"
	}
	return "error"
}
