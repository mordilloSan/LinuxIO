package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// These variables are intended to be set at build time via -ldflags.
// Example:
//
//	go build -ldflags "-X github.com/mordilloSan/LinuxIO/backend/indexer/internal/version.Version=v1.2.0 -X github.com/mordilloSan/LinuxIO/backend/indexer/internal/version.Commit=abc123 -X github.com/mordilloSan/LinuxIO/backend/indexer/internal/version.Date=2025-01-01T00:00:00Z"
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

type Info struct {
	Version string
	Commit  string
	Date    string
	Dirty   bool
}

func Get() Info {
	info := Info{
		Version: strings.TrimSpace(Version),
		Commit:  strings.TrimSpace(Commit),
		Date:    strings.TrimSpace(Date),
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.Date == "" {
					info.Date = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" {
					info.Dirty = true
				}
			}
		}
	}

	return info
}

func (i Info) String() string {
	v := i.Version
	if v == "" {
		v = "dev"
	}

	parts := []string{v}
	var meta []string
	if i.Commit != "" {
		meta = append(meta, "commit "+i.Commit)
	}
	if i.Date != "" {
		meta = append(meta, "built "+i.Date)
	}
	if i.Dirty {
		meta = append(meta, "dirty")
	}
	if len(meta) > 0 {
		parts = append(parts, "("+strings.Join(meta, ", ")+")")
	}
	return strings.Join(parts, " ")
}

func String() string {
	return fmt.Sprintf("indexer %s", Get().String())
}
