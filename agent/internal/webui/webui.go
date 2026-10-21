// Package webui embute o build do dashboard (web/ → static/) no binário.
// Sem `make web`, static/ só tem o README e o agente avisa que o dashboard
// não foi compilado.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var embedded embed.FS

// FS devolve os arquivos do dashboard, ou nil se o build não foi feito.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
