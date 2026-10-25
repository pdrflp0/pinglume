package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"pinglume/agent/internal/api"
	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/coverage"
	"pinglume/agent/internal/lock"
	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/runner"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
	"pinglume/agent/internal/webui"
)

// devTargets coleta --dev-target label=ip:porta. Enquanto os endpoints
// próprios não estão hospedados, o agente só mede alvos de desenvolvimento em
// loopback ou rede privada: nada de medir serviços de terceiros.
type devTargets []devTarget

type devTarget struct {
	Label string
	Addr  netip.AddrPort
}

func (d *devTargets) String() string { return fmt.Sprint(*d) }

func (d *devTargets) Set(v string) error {
	label, addr, ok := strings.Cut(v, "=")
	if !ok || label == "" {
		return errors.New("formato esperado label=ip:porta")
	}
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return fmt.Errorf("endereço %q: use IP literal e porta: %w", addr, err)
	}
	if !ap.Addr().IsLoopback() && !ap.Addr().IsPrivate() {
		return fmt.Errorf("alvo %s recusado: alvos de desenvolvimento precisam ser loopback ou rede privada", ap)
	}
	*d = append(*d, devTarget{Label: label, Addr: ap})
	return nil
}

// controller liga a API ao runner pelo canal de comandos.
type controller struct {
	r   *runner.Runner
	ctl chan runner.Command
}

func (c controller) State() string { return c.r.State() }

func (c controller) NetContext() netctx.Context { return c.r.NetContext() }

func (c controller) SetPaused(ctx context.Context, paused bool) error {
	done := make(chan error, 1)
	select {
	case c.ctl <- runner.Command{Pause: paused, Done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var c common
	c.register(fs)
	var cfg probeConfig
	fs.DurationVar(&cfg.tcpInterval, "tcp-interval", 10*time.Second, "intervalo da sonda TCP")
	fs.DurationVar(&cfg.tcpTimeout, "tcp-timeout", 3*time.Second, "timeout da sonda TCP")
	fs.DurationVar(&cfg.httpsInterval, "https-interval", 60*time.Second, "intervalo da sonda HTTPS")
	fs.DurationVar(&cfg.httpsTimeout, "https-timeout", 5*time.Second, "timeout da sonda HTTPS")
	fs.DurationVar(&cfg.dnsInterval, "dns-interval", 30*time.Second, "intervalo da sonda DNS")
	fs.DurationVar(&cfg.dnsTimeout, "dns-timeout", 3*time.Second, "timeout da sonda DNS")
	fs.StringVar(&cfg.devCA, "dev-ca", "", "certificado do probe-server local (PEM); ativa a sonda HTTPS nos --dev-target")
	fs.StringVar(&cfg.serverName, "dev-server-name", "probe.pinglume.test", "nome verificado no certificado do testbed")
	fs.StringVar(&cfg.zone, "dev-zone", "p.pinglume.test", "zona DNS do testbed")
	concurrency := fs.Int("concurrency", 2, "sondas simultâneas no máximo")
	fs.Var(&cfg.targets, "dev-target", "alvo TCP/HTTPS de desenvolvimento label=ip:porta (repetível)")
	fs.Var(&cfg.resolvers, "dev-dns", "servidor DNS de desenvolvimento label=ip:porta (repetível)")
	fs.Parse(args)

	if len(cfg.targets) == 0 && len(cfg.resolvers) == 0 {
		return errors.New("nenhum endpoint configurado: os endpoints próprios ainda não estão hospedados; " +
			"use --dev-target/--dev-dns com o probe-server local (veja README)")
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(c.dataDir, 0o700); err != nil {
		return err
	}
	lk, err := lock.Acquire(filepath.Join(c.dataDir, "agent.lock"))
	if err != nil {
		return err
	}
	defer lk.Release()

	token, err := api.LoadOrCreateToken(c.tokenPath())
	if err != nil {
		return fmt.Errorf("token da API: %w", err)
	}

	st, err := openStore(ctx, c.dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	clk := clock.System{}
	hostname, _ := os.Hostname()
	dev, err := st.EnsureDevice(ctx, store.Device{
		Name: hostname, OS: runtime.GOOS, Arch: runtime.GOARCH, AgentVersion: version,
	}, clk.Now())
	if err != nil {
		return err
	}

	jobs, err := buildJobs(ctx, st, cfg, clk.Now())
	if err != nil {
		return err
	}

	ctl := make(chan runner.Command)
	r := &runner.Runner{
		Store:     st,
		Tracker:   &coverage.Tracker{Store: st, Clock: clk, DeviceID: dev.ID, Heartbeat: 5 * time.Second},
		Scheduler: &sched.Scheduler{Store: st, Clock: clk, DeviceID: dev.ID, Concurrency: *concurrency, Jitter: 0.1, Log: log},
		Jobs:      jobs,
		Log:       log,
		Control:   ctl,
		Net:       &netctx.System{},
	}

	srv := &api.Server{Store: st, Ctl: controller{r: r, ctl: ctl}, Token: token, Port: c.port,
		Version: version, Static: webui.FS(), DeviceID: dev.ID}
	ln, err := srv.Listen()
	if err != nil {
		return fmt.Errorf("API local na porta %d: %w", c.port, err)
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("API local", "err", err)
		}
	}()
	if webui.FS() == nil {
		log.Warn("dashboard não compilado; a API funciona, mas `/` mostra só um aviso (rode `make web`)")
	}

	st.InsertSystemEvent(ctx, clk.Now(), "agent_start", version)
	log.Info("agente iniciado", "dados", c.dataDir, "sondas", len(jobs),
		"dashboard", fmt.Sprintf("http://127.0.0.1:%d (use `pinglume open`)", c.port))

	runErr := r.Run(ctx)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	httpSrv.Shutdown(stopCtx)
	st.InsertSystemEvent(stopCtx, clk.Now(), "agent_stop", "")
	log.Info("agente encerrado")
	return runErr
}
