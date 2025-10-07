package web

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend/*
var FrontendFS embed.FS

func UI() (fs.FS, error) {
	return fs.Sub(FrontendFS, "frontend")
}
