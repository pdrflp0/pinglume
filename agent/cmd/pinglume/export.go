package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	var c common
	c.register(fs)
	window := fs.Duration("window", 24*time.Hour, "período até agora (até 192h)")
	htmlOut := fs.String("html", "", "arquivo HTML de saída, ex.: relatorio.html")
	jsonOut := fs.String("json", "", "arquivo JSON de saída, ex.: relatorio.json")
	noIncidents := fs.Bool("no-incidents", false, "não incluir relatos")
	noComparisons := fs.Bool("no-comparisons", false, "não incluir comparações")
	notes := fs.Bool("notes", false, "incluir as observações dos relatos (redigidas)")
	fs.Parse(args)
	if *htmlOut == "" && *jsonOut == "" {
		return errors.New("informe --html e/ou --json com o caminho de saída")
	}

	now := time.Now()
	body := map[string]any{
		"from": now.Add(-*window).UTC().Format(time.RFC3339Nano), "to": now.UTC().Format(time.RFC3339Nano),
		"includeIncidents": !*noIncidents, "includeComparisons": !*noComparisons, "includeNotes": *notes,
	}
	var m struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	}
	if err := apiCallBody(c, "POST", "/api/v1/exports", body, &m); err != nil {
		return err
	}
	for _, out := range []struct{ format, path string }{{"html", *htmlOut}, {"json", *jsonOut}} {
		if out.path == "" {
			continue
		}
		data, err := apiDownload(c, fmt.Sprintf("/api/v1/exports/%s/download?format=%s", m.ID, out.format))
		if err != nil {
			return err
		}
		if out.format == "json" {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != m.SHA256 {
				return errors.New("o JSON recebido não confere com o SHA-256 do snapshot")
			}
		}
		if err := os.WriteFile(out.path, data, 0o600); err != nil {
			return err
		}
		fmt.Printf("gravado %s (%d bytes)\n", out.path, len(data))
	}
	fmt.Printf("snapshot %s · SHA-256 do JSON %s\n", m.ID, m.SHA256)
	fmt.Println("lembrete: um arquivo compartilhado não pode ser revogado. Confira o conteúdo antes de enviar.")
	return nil
}

// apiDownload devolve o corpo bruto de uma rota autenticada.
func apiDownload(c common, path string) ([]byte, error) {
	tok, err := os.ReadFile(c.tokenPath())
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d%s", c.port, path), nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("agente respondeu %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
