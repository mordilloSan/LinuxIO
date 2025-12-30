package config

// Environment modes
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Installation paths (production)
const (
	BinDir         = "/usr/local/bin"
	AuthHelperPath = BinDir + "/linuxio-auth"
)

// GitHub repository
const (
	RepoOwner = "mordilloSan"
	RepoName  = "LinuxIO"
)

// Build info - set at build time via ldflags:
// go build -ldflags "-X github.com/.../config.Version=v1.0.0"
var (
	Version      = "untracked"
	CommitSHA    = ""
	BuildTime    = ""
	BridgeSHA256 = "" // SHA256 hash of linuxio-bridge binary (set at build time)
)
