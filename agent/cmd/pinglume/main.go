package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"pinglume/agent/internal/store"
)

const (
	version     = "0.4.0"
	defaultPort = 47821
)

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
	case "open":
		err = cmdOpen(os.Args[2:])
	case "mark":
		err = cmdMark(os.Args[2:])
	case "incidents":
		err = cmdIncidents(os.Args[2:])
	case "storage":
		err = cmdStorage(os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	case "pause":
		err = cmdPause(os.Args[2:], true)
	case "resume":
		err = cmdPause(os.Args[2:], false)
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
  run      inicia a coleta e o dashboard local (Ctrl+C encerra)
  open     abre o dashboard no navegador com uma sessão nova
  status   resume medições e cobertura recentes (lê o banco; funciona com o agente parado)
  mark       registra "travou agora" (um relato, não uma medição)
  incidents  lista os relatos e a análise de cada um
  storage    mostra uso do banco, retenção e manutenções recentes
  export     gera relatório HTML/JSON redigido (--html, --json)
  pause    pausa novas medições do agente em execução
  resume   retoma as medições
  version  mostra a versão`)
}

type common struct {
	dataDir string
	port    int
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.dataDir, "data-dir", defaultDataDir(), "diretório de dados")
	fs.IntVar(&c.port, "port", defaultPort, "porta da API local (só 127.0.0.1)")
}

func (c *common) tokenPath() string { return filepath.Join(c.dataDir, "api-token") }

func defaultDataDir() string {
	base, err := os.UserConfigDir()
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
