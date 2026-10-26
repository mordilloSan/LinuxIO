package version

import (
	"strconv"
	"strings"
)

// IsNewer returns true when latest should be considered newer than current.
// Versions may include optional "dev-" and "v" prefixes and a "-pre-release"
// suffix, which sorts below the same version without one.
func IsNewer(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}

	current, currentIsDev := strings.CutPrefix(current, "dev-")
	latest, latestIsDev := strings.CutPrefix(latest, "dev-")

	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	latest, latestPre, latestHasPre := strings.Cut(latest, "-")
	current, currentPre, currentHasPre := strings.Cut(current, "-")

	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	cmp := compareVersionParts(latestParts, currentParts)
	if cmp != 0 {
		return cmp > 0
	}

	if latestHasPre != currentHasPre {
		return currentHasPre
	}
	if latestPre != currentPre {
		return latestPre > currentPre
	}

	if currentIsDev && !latestIsDev {
		return true
	}
	return false
}

func compareVersionParts(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ai, err1 := strconv.Atoi(a[i])
		bi, err2 := strconv.Atoi(b[i])
		if err1 != nil || err2 != nil {
			if a[i] > b[i] {
				return 1
			}
			if a[i] < b[i] {
				return -1
			}
			continue
		}
		if ai > bi {
			return 1
		}
		if ai < bi {
			return -1
		}
	}
	return len(a) - len(b)
}
