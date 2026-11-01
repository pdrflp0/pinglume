package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pinglume/agent/internal/demo"
	"pinglume/agent/internal/retention"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

func cmdDemo(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	var c common
	c.register(fs)
	keep := fs.Bool("keep", false, "mantém o banco de demonstração existente em vez de gerar um novo")
	fs.Parse(args)
	c.demo = true
	c.resolve()

	if c.dataDir == defaultDataDir() {
		return errors.New("o modo demonstração não pode usar o diretório de dados reais")
	}
	if !*keep {
		if err := resetDemo(c.dataDir); err != nil {
			return err
		}
	}
	fmt.Printf("MODO DEMONSTRAÇÃO: dados simulados em %s, dashboard em http://127.0.0.1:%d (`pinglume open --demo`)\n", c.dataDir, c.port)
	return startAgent(agentOptions{common: c, mode: store.ModeDemo, concurrency: 2, policy: retention.DefaultPolicy(),
		deviceName: demo.DeviceName,
		prepare: func(ctx context.Context, st *store.Store, deviceID string, now time.Time) error {
			if _, ok, err := st.OldestObservation(ctx); err != nil || ok {
				return err
			}
			return demo.NewScenario(now).Generate(ctx, st, deviceID)
		},
		jobs: func(ctx context.Context, st *store.Store, _ time.Time) ([]sched.Job, error) {
			return demo.Jobs(ctx, st, time.Now)
		},
		net: demo.Net{}})
}

func resetDemo(dir string) error {
	path := filepath.Join(dir, "pinglume.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	ctx := context.Background()
	st, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	mode, err := st.Mode(ctx)
	st.Close()
	if err != nil {
		return err
	}
	if mode != store.ModeDemo {
		return store.ErrRealData
	}
	for _, f := range []string{"pinglume.db", "pinglume.db-wal", "pinglume.db-shm"} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
