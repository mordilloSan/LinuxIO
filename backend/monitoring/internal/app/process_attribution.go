package app

import (
	"os"
	"strings"
	"unicode"

	container "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
)

// processAttributionResolver maps host PIDs to identities reported by a
// Docker-compatible runtime. Cgroup files are best-effort: unreadable or
// ambiguous paths deliberately produce no attribution.
type processAttributionResolver struct {
	identities []container.Identity
}

func newProcessAttributionResolver(identities []container.Identity) *processAttributionResolver {
	return &processAttributionResolver{identities: append([]container.Identity(nil), identities...)}
}

func (r *processAttributionResolver) resolve(pid int32) (container.Identity, bool) {
	b, err := os.ReadFile("/proc/" + itoaPID(pid) + "/cgroup")
	if err != nil {
		return container.Identity{}, false
	}
	return r.resolveCgroup(string(b))
}

//nolint:gocognit // Cgroup matching must resolve nested identities and reject equal-depth ambiguity.
func (r *processAttributionResolver) resolveCgroup(content string) (container.Identity, bool) {
	if r == nil || len(r.identities) == 0 {
		return container.Identity{}, false
	}
	// A path can contain nested cgroups. Prefer the identity found in the
	// deepest path component; ties are ambiguous and intentionally rejected.
	bestDepth := -1
	var best []container.Identity
	for line := range strings.SplitSeq(content, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		for _, identity := range r.identities {
			if identity.FullID == "" && identity.ID == "" {
				continue
			}
			if depth, matches := cgroupIdentityMatchDepth(path, identity); matches {
				if depth > bestDepth {
					bestDepth, best = depth, []container.Identity{identity}
				} else if depth == bestDepth {
					duplicate := false
					for _, existing := range best {
						if existing.FullID == identity.FullID && existing.ID == identity.ID {
							duplicate = true
							break
						}
					}
					if !duplicate {
						best = append(best, identity)
					}
				}
			}
		}
	}
	if len(best) != 1 {
		return container.Identity{}, false
	}
	return best[0], true
}

func cgroupIdentityMatchDepth(path string, identity container.Identity) (int, bool) {
	full := strings.ToLower(strings.TrimSpace(identity.FullID))
	short := strings.ToLower(strings.TrimSpace(identity.ID))
	bestDepth := -1
	for depth, component := range strings.Split(strings.Trim(path, "/"), "/") {
		for _, token := range hexRuns(component) {
			if (full != "" && token == full) || (short != "" && token == short) {
				bestDepth = depth
			}
		}
	}
	return bestDepth, bestDepth >= 0
}

func hexRuns(path string) []string {
	var runs []string
	start := -1
	for i, r := range path {
		if unicode.Is(unicode.ASCII_Hex_Digit, r) {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			runs = append(runs, strings.ToLower(path[start:i]))
			start = -1
		}
	}
	if start >= 0 {
		runs = append(runs, strings.ToLower(path[start:]))
	}
	return runs
}

func itoaPID(pid int32) string {
	if pid < 0 {
		return ""
	}
	var b [12]byte
	i := len(b)
	for pid > 0 {
		i--
		b[i] = byte('0' + pid%10)
		pid /= 10
	}
	if i == len(b) {
		return "0"
	}
	return string(b[i:])
}
