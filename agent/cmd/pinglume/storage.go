package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"pinglume/agent/internal/retention"
	"pinglume/agent/internal/storage"
)

func cmdStorage(args []string) error {
	fs := flag.NewFlagSet("storage", flag.ExitOnError)
	var c common
	c.register(fs)
	pol := retention.DefaultPolicy()
	fs.DurationVar(&pol.Raw, "retention", pol.Raw, "retenção dos dados brutos configurada no agente")
	fs.DurationVar(&pol.Aggregates, "aggregate-retention", pol.Aggregates, "retenção dos agregados configurada no agente")
	limitMB := fs.Int64("disk-limit-mb", pol.LimitBytes>>20, "limite configurado no agente, em MB")
	fs.Parse(args)
	pol.LimitBytes = *limitMB << 20

	ctx := context.Background()
	st, err := openStore(ctx, c.dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	info, err := storage.Load(ctx, st, pol, time.Now())
	if err != nil {
		return err
	}
	mb := func(b int64) string { return strings.Replace(fmt.Sprintf("%.1f MB", float64(b)/(1<<20)), ".", ",", 1) }
	fmt.Printf("Banco: %s usados (%s no arquivo)", mb(info.Usage.UsedBytes), mb(info.Usage.FileBytes))
	if info.LimitBytes > 0 {
		fmt.Printf(" · limite %s", mb(info.LimitBytes))
	}
	fmt.Printf("\nMedições brutas: guardadas por %s", human(info.RawRetentionMS))
	if info.OldestRaw != nil {
		fmt.Printf(" · mais antiga %s", info.OldestRaw.Local().Format("02/01 15:04"))
	}
	fmt.Printf("\nAgregados por minuto: guardados por %s", human(info.AggRetentionMS))
	if info.OldestAggregate != nil {
		fmt.Printf(" · mais antigo %s", info.OldestAggregate.Local().Format("02/01 15:04"))
	}
	fmt.Println()
	if len(info.RecentMaintenance) == 0 {
		fmt.Println("Nenhuma compactação ou descarte registrado nos últimos 30 dias.")
		return nil
	}
	fmt.Println("\nManutenção recente:")
	for _, e := range info.RecentMaintenance {
		fmt.Printf("  %s  %s  %v\n", e.At.Local().Format("02/01 15:04:05"), e.Kind, e.Detail)
	}
	return nil
}

func human(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d >= 48*time.Hour && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d dias", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d h", int(d/time.Hour))
	}
	return d.String()
}
