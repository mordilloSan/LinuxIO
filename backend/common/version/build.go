package version

// Build identity, set at build time via ldflags:
// go build -ldflags "-X github.com/mordilloSan/LinuxIO/backend/common/version.Version=v1.0.0"
var (
	Version      = "untracked"
	BridgeSHA256 = "" // SHA256 hash of linuxio-bridge binary
)
