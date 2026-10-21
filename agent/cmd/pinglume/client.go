package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// apiCall faz um pedido autenticado à API local do agente em execução.
func apiCall(c common, method, path string, out any) error {
	b, err := os.ReadFile(c.tokenPath())
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("token não encontrado: o agente já rodou neste diretório de dados?")
	}
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", c.port, path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("nenhum agente respondendo em 127.0.0.1:%d; inicie com `pinglume run`", c.port)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("agente respondeu %d: %s", resp.StatusCode, e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func cmdPause(args []string, pause bool) error {
	name := map[bool]string{true: "pause", false: "resume"}[pause]
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	var c common
	c.register(fs)
	fs.Parse(args)

	var resp struct{ State string }
	if err := apiCall(c, "POST", "/api/v1/"+name, &resp); err != nil {
		return err
	}
	// O agente só responde depois de aplicar e gravar o novo estado.
	if resp.State == "paused" {
		fmt.Println("medição pausada; nenhuma nova sonda será iniciada até `pinglume resume`")
	} else {
		fmt.Println("medição retomada")
	}
	return nil
}

// cmdOpen pede um código de uso único (válido por 60 s) e abre o navegador.
// O código vira um cookie de sessão; o token em si nunca vai para o navegador.
func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	var c common
	c.register(fs)
	printOnly := fs.Bool("print", false, "só imprime o endereço, sem abrir o navegador")
	fs.Parse(args)

	var resp struct{ Code string }
	if err := apiCall(c, "POST", "/api/v1/auth/codes", &resp); err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/auth?code=%s", c.port, resp.Code)
	if *printOnly {
		fmt.Println(url)
		return nil
	}
	fmt.Println("abrindo o dashboard (link de uso único, válido por 60 s)")
	return exec.Command("open", url).Run() // macOS; outras plataformas no Nível 2
}
