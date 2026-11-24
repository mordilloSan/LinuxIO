package process

import (
	"os"
	"strings"

	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
)

// processAttributionResolver maps host PIDs to the containers a runtime
// running as root reports. Cgroup files are best-effort: unreadable or
// ambiguous paths deliberately produce no attribution.
type processAttributionResolver struct {
	byFullID map[string]containermodel.Identity
}

func newProcessAttributionResolver(identities []containermodel.Identity) *processAttributionResolver {
	byFullID := make(map[string]containermodel.Identity, len(identities))
	for _, identity := range identities {
		if id := strings.ToLower(strings.TrimSpace(identity.FullID)); id != "" {
			byFullID[id] = identity
		}
	}
	return &processAttributionResolver{byFullID: byFullID}
}

func (r *processAttributionResolver) resolve(pid int32) (containermodel.Identity, bool) {
	b, err := os.ReadFile("/proc/" + itoaPID(pid) + "/cgroup")
	if err != nil {
		return containermodel.Identity{}, false
	}
	return r.resolveCgroup(string(b))
}

// resolveCgroup attributes a process to the container whose full ID names the
// cgroup the runtime made for it, at the place only root can create it:
// /system.slice/docker-<id>.scope or /docker/<id> for Docker's two cgroup
// drivers, /machine.slice/libpod-<id>.scope or /libpod_parent/libpod-<id> for
// Podman's. A cgroup a local user makes under their own slice is never
// attributed, whatever its name. Lines that name two containers are ambiguous
// and attribute nothing.
func (r *processAttributionResolver) resolveCgroup(content string) (containermodel.Identity, bool) {
	if r == nil || len(r.byFullID) == 0 {
		return containermodel.Identity{}, false
	}
	found := ""
	for line := range strings.SplitSeq(content, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		id, ok := runtimeContainerID(parts[2])
		if !ok {
			continue
		}
		if found != "" && found != id {
			return containermodel.Identity{}, false
		}
		found = id
	}
	identity, ok := r.byFullID[found]
	return identity, ok
}

// runtimeContainerID reads the container ID from a cgroup path whose first two
// components are where a root runtime puts a container; deeper components
// are the container's own cgroups.
func runtimeContainerID(path string) (string, bool) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 3)
	if len(parts) < 2 {
		return "", false
	}
	var id string
	switch parts[0] {
	case "system.slice":
		id = trimAffixes(parts[1], "docker-", ".scope")
	case "docker":
		id = parts[1]
	case "machine.slice":
		id = trimAffixes(parts[1], "libpod-", ".scope")
	case "libpod_parent":
		id = trimAffixes(parts[1], "libpod-", "")
	}
	return id, isFullContainerID(id)
}

// trimAffixes returns name without the prefix and suffix, or "" when it lacks
// either.
func trimAffixes(name, prefix, suffix string) string {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return ""
	}
	rest, ok = strings.CutSuffix(rest, suffix)
	if !ok {
		return ""
	}
	return rest
}

// isFullContainerID reports a 64-digit lowercase hex ID, the form runtimes
// give the cgroups they create.
func isFullContainerID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
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
