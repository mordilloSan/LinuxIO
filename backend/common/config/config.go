package config

// Environment modes
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Installation paths
const (
	BinDir         = "/usr/local/bin"
	BinPath        = BinDir + "/linuxio"
	BridgePath     = BinDir + "/linuxio-bridge"
	AuthHelperPath = BinDir + "/linuxio-auth-helper"
)

// GitHub repository
const (
	RepoOwner = "mordilloSan"
	RepoName  = "LinuxIO"
)

// Version is set at build time via ldflags:
// go build -ldflags "-X github.com/.../config.Version=v1.0.0"
var Version = "untracked"
