// probe-server é o endpoint das sondas Pinglume. Nesta fatia ele só aceita
// conexões TCP e as fecha; HTTPS, DNS curinga e eco UDP chegam nas fatias
// F5 e F12. A flag -fault permite reproduzir falhas no testbed local.
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("tcp", "127.0.0.1:9443", "endereço TCP de escuta")
	fault := flag.String("fault", "none", "falha simulada: none | close (encerra a escuta após -fault-after)")
	faultAfter := flag.Duration("fault-after", 0, "atraso antes de aplicar a falha")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("escuta falhou", "err", err)
		os.Exit(1)
	}
	log.Info("probe-server ouvindo", "tcp", ln.Addr().String(), "fault", *fault)

	var accepted atomic.Int64
	if *fault == "close" {
		go func() {
			time.Sleep(*faultAfter)
			log.Warn("falha simulada: escuta encerrada; novas conexões serão recusadas")
			ln.Close()
		}()
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Info("escuta encerrada", "aceitas", accepted.Load())
			// Mantém o processo vivo para a falha parecer porta fechada no host.
			select {}
		}
		accepted.Add(1)
		c.Close()
	}
}
