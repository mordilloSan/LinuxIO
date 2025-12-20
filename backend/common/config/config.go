package config

import (
	"os"
	"path/filepath"
)

// Environment modes
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Installation paths (production)
const (
	BinDir         = "/usr/local/bin"
	AuthHelperPath = BinDir + "/linuxio-auth-helper"
)

// Development paths
const (
	DevDir = "/tmp/linuxio/dev"
)

// BinPath and BridgePath are determined at runtime based on execution context
var (
	BinPath    string
	BridgePath string
)

func init() {
	exe, err := os.Executable()
	if err != nil {
		// Fallback to production paths
		BinPath = BinDir + "/linuxio"
		BridgePath = BinDir + "/linuxio-bridge"
		return
	}

	// Resolve symlinks to get the real path
	exe, _ = filepath.EvalSymlinks(exe)
	exeDir := filepath.Dir(exe)

	// If running from production directory, use production paths
	if exeDir == BinDir {
		BinPath = BinDir + "/linuxio"
		BridgePath = BinDir + "/linuxio-bridge"
	} else {
		// Development mode
		BinPath = "./linuxio"
		BridgePath = DevDir + "/linuxio-bridge"
	}
}

// GitHub repository
const (
	RepoOwner = "mordilloSan"
	RepoName  = "LinuxIO"
)

// Build info - set at build time via ldflags:
// go build -ldflags "-X github.com/.../config.Version=v1.0.0"
var (
	Version    = "untracked"
	CommitSHA  = ""
	BuildTime  = ""
)
