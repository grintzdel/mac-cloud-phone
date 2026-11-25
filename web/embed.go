package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html dist/app.js
var files embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(files, ".")
	return http.FileServer(http.FS(sub))
}
