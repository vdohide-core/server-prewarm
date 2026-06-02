package handlers

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFS embed.FS

//go:embed templates/*
var templateFS embed.FS

// GetStaticFS returns the HTTP file system for static files (CSS, JS)
func GetStaticFS() http.FileSystem {
	fsys, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FS(fsys)
}
