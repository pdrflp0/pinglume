package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"pinglume/agent/internal/summary"
)

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	var c common
	c.register(fs)
	window := fs.Duration("window", 15*time.Minute, "período resumido")
	fs.Parse(args)

	ctx := context.Background()
	st, err := openStore(ctx, c.dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	now := time.Now()
	s, err := summary.Build(ctx, st, now.Add(-*window), now, summary.Options{})
	if err != nil {
		return err
	}
	fmt.Printf("Últimos %s (%s–%s)\n\n", *window, s.From.Format("15:04:05"), s.To.Format("15:04:05"))

	fmt.Println("Cobertura do agente:")
	for _, k := range []string{"measuring", "paused", "suspended", "agent_down", "probe_unavailable", summary.Unrecorded} {
		if ms := s.StateMS[k]; ms > 0 {
			fmt.Printf("  %-18s %s\n", stateLabel(k), (time.Duration(ms) * time.Millisecond).Round(time.Second))
		}
	}
	if len(s.Gaps) > 0 {
		fmt.Println("\nPeríodos sem medição:")
		for _, g := range s.Gaps {
			fmt.Printf("  %s–%s  %s: %s\n", g.Start.Local().Format("15:04:05"), g.End.Local().Format("15:04:05"),
				stateLabel(g.State), g.Reason)
		}
	}
	fmt.Println()

	if len(s.Probes) == 0 {
		fmt.Println("Sem observações no período. Isso significa sem cobertura, não que a conexão esteve boa.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ENDPOINT\tSONDA\tALVO\tEXECUÇÕES\tCOBERTURA\tBACKOFF\tOK\tFALHAS\tP50\tP95\tÚLTIMA")
	for _, p := range s.Probes {
		bo := "-"
		if p.BackoffMS > 0 {
			bo = (time.Duration(p.BackoffMS) * time.Millisecond).Round(time.Second).String()
		}
		fmt.Fprintf(w, "%s\t%s v%d\t%s\t%d\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			p.Endpoint, p.Kind, p.ProtocolVersion, p.Target, p.Total, ratio(p.Coverage), bo, p.OK,
			failures(p.Failures), approx(micros(p.P50US), p.Approximate), approx(micros(p.P95US), p.Approximate),
			p.LastStarted.Local().Format("15:04:05"))
	}
	w.Flush()
	fmt.Println("\nCOBERTURA: execuções concluídas ÷ esperadas durante o tempo medindo.")
	fmt.Println("BACKOFF: tempo com intervalo ampliado após falhas seguidas (reduz a cobertura de propósito).")
	fmt.Println("P50/P95, só execuções ok — tcp: handshake; dns: resolução de nome único; https: requisição completa com conexão nova.")
	fmt.Println("≈: percentis aproximados, o período inclui dados antigos compactados por minuto (contagens exatas).")
	fmt.Println("Falhas são rotuladas pelo protocolo da sonda; não são perda de pacotes IP.")
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
	case summary.Unrecorded:
		return "sem registro"
	}
	return s
}

func ratio(r *float64) string {
	if r == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *r*100)
}

func micros(us *int64) string {
	if us == nil {
		return "-"
	}
	return (time.Duration(*us) * time.Microsecond).Round(10 * time.Microsecond).String()
}

func failures(by map[string]int) string {
	var parts []string
	for k, v := range by {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	if len(parts) == 0 {
		return "-"
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func approx(v string, a bool) string {
	if a && v != "-" {
		return "≈" + v
	}
	return v
}
