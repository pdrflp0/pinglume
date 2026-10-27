package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var embedded embed.FS

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
