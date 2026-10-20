package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/coverage"
	"pinglume/agent/internal/lock"
	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/runner"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "pause":
		err = cmdSignal(os.Args[2:], "pause", syscall.SIGUSR1, "1")
	case "resume":
		err = cmdSignal(os.Args[2:], "resume", syscall.SIGUSR2, "0")
	case "version":
		fmt.Println("pinglume", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `uso: pinglume <comando> [opções]

comandos:
  run      inicia a coleta (Ctrl+C encerra)
  status   resume medições e cobertura recentes
  pause    pausa novas medições do agente em execução
  resume   retoma as medições
  version  mostra a versão`)
}

func defaultDataDir() string {
	base, err := os.UserConfigDir() // macOS: ~/Library/Application Support
	if err != nil {
		return ".pinglume"
	}
	return filepath.Join(base, "Pinglume")
}

func openStore(ctx context.Context, dataDir string) (*store.Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	return store.Open(ctx, filepath.Join(dataDir, "pinglume.db"))
}

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

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "diretório de dados")
	interval := fs.Duration("tcp-interval", 10*time.Second, "intervalo da sonda TCP")
	timeout := fs.Duration("tcp-timeout", 3*time.Second, "timeout da sonda TCP")
	concurrency := fs.Int("concurrency", 2, "sondas simultâneas no máximo")
	var targets devTargets
	fs.Var(&targets, "dev-target", "alvo de desenvolvimento label=ip:porta (repetível)")
	fs.Parse(args)

	if len(targets) == 0 {
		return errors.New("nenhum endpoint configurado: os endpoints próprios ainda não estão hospedados; " +
			"use --dev-target com o probe-server local (veja README)")
	}
	if *timeout >= *interval {
		return errors.New("tcp-timeout precisa ser menor que tcp-interval")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}
	lk, err := lock.Acquire(filepath.Join(*dataDir, "agent.lock"))
	if err != nil {
		return err
	}
	defer lk.Release()

	st, err := openStore(ctx, *dataDir)
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

	var jobs []sched.Job
	for _, t := range targets {
		p := probe.TCPConnect{Addr: t.Addr.String(), Timeout: *timeout}
		def, err := st.EnsureProbeDefinition(ctx,
			store.Endpoint{Label: t.Label, Host: t.Addr.Addr().String(), Owner: "dev"},
			store.ProbeDefinition{
				Kind: p.Kind(), Target: t.Addr.String(), Interval: *interval, Timeout: *timeout,
				ProtocolVersion: p.ProtocolVersion(),
			}, clk.Now())
		if err != nil {
			return err
		}
		jobs = append(jobs, sched.Job{Def: def, Prober: p})
	}

	st.InsertSystemEvent(ctx, clk.Now(), "agent_start", version)
	log.Info("agente iniciado", "dados", *dataDir, "sondas", len(jobs), "pid", os.Getpid())

	// Pausa/retomada chegam por sinais enviados por `pinglume pause|resume`.
	// A API local autenticada (F3) passará a ser o canal principal.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGUSR1, syscall.SIGUSR2)
	defer signal.Stop(sigs)
	pause, resume := make(chan struct{}), make(chan struct{})
	go func() {
		for sg := range sigs {
			ch := pause
			if sg == syscall.SIGUSR2 {
				ch = resume
			}
			select {
			case ch <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()

	r := &runner.Runner{
		Store:     st,
		Tracker:   &coverage.Tracker{Store: st, Clock: clk, DeviceID: dev.ID, Heartbeat: 5 * time.Second},
		Scheduler: &sched.Scheduler{Store: st, Clock: clk, DeviceID: dev.ID, Concurrency: *concurrency, Jitter: 0.1, Log: log},
		Jobs:      jobs,
		Log:       log,
		Pause:     pause,
		Resume:    resume,
	}
	runErr := r.Run(ctx)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	st.InsertSystemEvent(stopCtx, clk.Now(), "agent_stop", "")
	log.Info("agente encerrado")
	return runErr
}

func cmdSignal(args []string, name string, sig syscall.Signal, want string) error {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "diretório de dados")
	fs.Parse(args)

	pid, ok, err := lock.HolderPID(filepath.Join(*dataDir, "agent.lock"))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("nenhum agente em execução neste diretório de dados")
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("sinal para o agente (pid %d): %w", pid, err)
	}
	// Confirma lendo o estado gravado pelo agente, em vez de presumir sucesso.
	ctx := context.Background()
	st, err := openStore(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v, _, _ := st.GetState(ctx, "paused"); v == want {
			if want == "1" {
				fmt.Println("medição pausada; nenhuma nova sonda será iniciada até `pinglume resume`")
			} else {
				fmt.Println("medição retomada")
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("o agente não confirmou a mudança em 2 s")
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "diretório de dados")
	window := fs.Duration("window", 15*time.Minute, "período resumido")
	fs.Parse(args)

	ctx := context.Background()
	st, err := openStore(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	now := time.Now()
	since := now.Add(-*window)
	fmt.Printf("Últimos %s (%s–%s)\n\n", *window, since.Format("15:04:05"), now.Format("15:04:05"))

	ivs, err := st.Intervals(ctx, since, now)
	if err != nil {
		return err
	}
	byState := map[string]time.Duration{}
	var covered time.Duration
	for _, iv := range ivs {
		byState[iv.State] += iv.Duration()
		covered += iv.Duration()
	}
	byState["sem registro"] = max(0, now.Sub(since)-covered)
	fmt.Println("Cobertura do agente:")
	for _, k := range []string{"measuring", "paused", "suspended", "agent_down", "probe_unavailable", "sem registro"} {
		if d := byState[k]; d > 0 {
			fmt.Printf("  %-18s %s\n", stateLabel(k), d.Round(time.Second))
		}
	}
	var gaps []string
	for _, iv := range ivs {
		if iv.State != "measuring" {
			gaps = append(gaps, fmt.Sprintf("  %s–%s  %s: %s", iv.Start.Local().Format("15:04:05"),
				iv.End.Local().Format("15:04:05"), stateLabel(iv.State), iv.Reason))
		}
	}
	if len(gaps) > 0 {
		fmt.Println("\nPeríodos sem medição:")
		for _, g := range gaps {
			fmt.Println(g)
		}
	}
	fmt.Println()

	sums, err := st.Summaries(ctx, since)
	if err != nil {
		return err
	}
	if len(sums) == 0 {
		fmt.Println("Sem observações no período. Isso significa sem cobertura, não que a conexão esteve boa.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ENDPOINT\tSONDA\tALVO\tEXECUÇÕES\tCOBERTURA\tOK\tFALHAS\tP50\tP95\tÚLTIMA")
	for _, s := range sums {
		fmt.Fprintf(w, "%s\t%s v%d\t%s\t%d\t%s\t%d\t%s\t%s\t%s\t%s\n",
			s.EndpointLabel, s.Kind, s.ProtocolVersion, s.Target, s.Total,
			coverageRatio(s, byState["measuring"]), s.ByStatus["ok"],
			failures(s.ByStatus), pct(s.OKDurations, 0.50), pct(s.OKDurations, 0.95),
			s.LastStarted.Local().Format("15:04:05"))
	}
	w.Flush()
	fmt.Println("\nCOBERTURA: execuções concluídas ÷ esperadas durante o tempo medindo.")
	fmt.Println("P50/P95: tempo de handshake TCP só das execuções ok. Falhas são rotuladas pelo protocolo, não são perda de pacotes IP.")
	return nil
}

func stateLabel(s string) string {
	switch s {
	case "measuring":
		return "medindo"
	case "paused":
		return "pausado"
	case "suspended":
		return "suspenso"
	case "agent_down":
		return "agente parado"
	case "probe_unavailable":
		return "sonda indisponível"
	}
	return s
}

func coverageRatio(s store.ProbeSummary, measuring time.Duration) string {
	if measuring <= 0 || s.Interval <= 0 {
		return "-"
	}
	expected := float64(measuring) / float64(s.Interval)
	if expected < 1 {
		return "-"
	}
	done := float64(s.Total - s.ByStatus["cancelled"])
	return fmt.Sprintf("%.0f%%", min(1, done/expected)*100)
}

func failures(by map[string]int) string {
	var parts []string
	for k, v := range by {
		if k != "ok" {
			parts = append(parts, fmt.Sprintf("%s=%d", k, v))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// pct usa o método do posto mais próximo sobre durações já ordenadas.
func pct(sorted []time.Duration, p float64) string {
	if len(sorted) == 0 {
		return "-"
	}
	i := int(float64(len(sorted)-1)*p + 0.5)
	return sorted[i].Round(10 * time.Microsecond).String()
}
