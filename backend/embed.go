package embed

import "embed"

// Frontend Embeding //

//go:embed all:frontend/assets/*
var StaticFS embed.FS

// Vite build manifest as bytes
//
//go:embed all:frontend/.vite/manifest.json
var ViteManifest []byte

// PWA manifest and all favicon PNGs
//
//go:embed all:frontend/manifest.json all:frontend/favicon-*.png
var PWAManifest embed.FS

// Backend Default Config Embeding //

//go:embed internal/config/filebrowserConfig.yaml
var DefaultFilebrowserConfig []byte

//go:embed internal/config/themeConfig.yaml
var DefaultThemeConfig []byte

//go:embed internal/config/dockerConfig.yaml
var DefaultDockerConfig []byte
