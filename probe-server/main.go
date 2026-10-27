package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var payload = []byte(strings.Repeat("pinglume-probe-v1 ", 4)[:63] + "\n")

func main() {
	addr := flag.String("tcp", "127.0.0.1:9443", "endereço TCP/HTTPS")
	dnsAddr := flag.String("dns", "127.0.0.1:15353", "endereço UDP do DNS (vazio desativa); evite 5353, que é do mDNS")
	zone := flag.String("zone", "p.pinglume.test", "zona DNS atendida")
	answer := flag.String("answer", "127.0.0.1", "endereço IPv4 devolvido nas respostas A")
	adminAddr := flag.String("admin", "127.0.0.1:9900", "API de falhas (só loopback; vazio desativa)")
	tlsDir := flag.String("tls-dir", "testbed/tls", "onde fica o certificado do testbed")
	serverName := flag.String("server-name", "probe.pinglume.test", "nome no certificado")
	initial := flag.String("faults", "", `estado inicial em JSON, ex.: {"dns":"drop"}`)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	fail := func(msg string, err error) {
		logger.Error(msg, "err", err)
		os.Exit(1)
	}

	faults := &Faults{f: defaultFaults()}
	if *initial != "" {
		h := adminHandler(faults, func(FaultState) {})
		req, _ := http.NewRequest("PUT", "/faults", strings.NewReader(*initial))
		rec := &statusRecorder{}
		h.ServeHTTP(rec, req)
		if rec.status != http.StatusOK {
			fail("-faults inválido", errors.New(string(rec.body)))
		}
	}

	cert, certPath, err := loadOrCreateCert(*tlsDir, *serverName)
	if err != nil {
		fail("certificado", err)
	}
	ansAddr, err := netip.ParseAddr(*answer)
	if err != nil || !ansAddr.Is4() {
		fail("-answer precisa ser IPv4", err)
	}

	srv := &probeServer{
		addr:   *addr,
		faults: faults,
		log:    logger,
		http: &http.Server{
			Handler:           probeHandler(faults),
			ReadHeaderTimeout: 5 * time.Second,

			ErrorLog: log.New(io.Discard, "", 0),
		},
		tlsConf: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}
	if err := srv.apply(faults.Get()); err != nil {
		fail("escuta TCP", err)
	}

	if *dnsAddr != "" {
		pc, err := net.ListenPacket("udp", *dnsAddr)
		if err != nil {
			fail("escuta DNS", err)
		}
		go serveDNS(pc, strings.ToLower(*zone), ansAddr, faults)
	}

	if *adminAddr != "" {
		ap, err := netip.ParseAddrPort(*adminAddr)
		if err != nil || !ap.Addr().IsLoopback() {
			fail("-admin precisa ser loopback", err)
		}
		admin := &http.Server{Addr: *adminAddr, ReadHeaderTimeout: 5 * time.Second,
			Handler: adminHandler(faults, func(s FaultState) {
				if err := srv.apply(s); err != nil {
					logger.Error("aplicar falha", "err", err)
				}
				logger.Warn("falhas atualizadas", "dns", s.DNS, "https", s.HTTPS, "atraso", s.HTTPSDelay, "listener", s.Listener)
			})}
		go admin.ListenAndServe()
	}

	logger.Info("probe-server pronto", "tcp+https", *addr, "dns", *dnsAddr, "zona", *zone,
		"admin", *adminAddr, "certificado", certPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

type probeServer struct {
	mu      sync.Mutex
	addr    string
	ln      net.Listener
	faults  *Faults
	log     *slog.Logger
	http    *http.Server
	tlsConf *tls.Config
}

func (p *probeServer) apply(s FaultState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case s.Listener == "closed" && p.ln != nil:
		p.ln.Close()
		p.ln = nil
	case s.Listener == "open" && p.ln == nil:
		ln, err := net.Listen("tcp", p.addr)
		if err != nil {
			return err
		}
		p.ln = ln
		go p.http.Serve(tls.NewListener(ln, p.tlsConf))
	}
	return nil
}

func probeHandler(f *Faults) http.Handler {
	limiter := newIPLimiter(20, time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /p", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !limiter.allow(host) {
			http.Error(w, "limite de requisições", http.StatusTooManyRequests)
			return
		}
		if d := f.httpsDelay(); d > 0 {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		switch f.Get().HTTPS {
		case "error500":
			http.Error(w, "falha simulada", http.StatusInternalServerError)
			return
		case "hang":
			<-r.Context().Done()
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain")
		w.Write(payload)
	})
	return mux
}

type ipLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	start  time.Time
	counts map[string]int
}

func newIPLimiter(max int, window time.Duration) *ipLimiter {
	return &ipLimiter{max: max, window: window, start: time.Now(), counts: map[string]int{}}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.start) > l.window {
		l.start, l.counts = time.Now(), map[string]int{}
	}
	l.counts[ip]++
	return l.counts[ip] <= l.max
}

type statusRecorder struct {
	status int
	body   []byte
	h      http.Header
}

func (s *statusRecorder) Header() http.Header {
	if s.h == nil {
		s.h = http.Header{}
	}
	return s.h
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	s.body = append(s.body, b...)
	return len(b), nil
}
func (s *statusRecorder) WriteHeader(code int) { s.status = code }
