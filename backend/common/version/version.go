package version

// Installation paths
const (
	BinDir = "/usr/local/bin"
)

// GitHub repository
const (
	RepoOwner = "mordilloSan"
	RepoName  = "LinuxIO"
)

// Build info - set at build time via ldflags:
// go build -ldflags "-X github.com/.../version.Version=v1.0.0"
var (
	Version      = "untracked"
	CommitSHA    = ""
	BuildTime    = ""
	BridgeSHA256 = "" // SHA256 hash of linuxio-bridge binary (set at build time)
)
