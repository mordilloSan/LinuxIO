package versioncmp

import (
	"strconv"
	"strings"
)

// IsNewer returns true when latest should be considered newer than current.
// Versions may include optional "dev-" and "v" prefixes.
func IsNewer(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}

	current, currentIsDev := strings.CutPrefix(current, "dev-")
	latest, latestIsDev := strings.CutPrefix(latest, "dev-")

	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(current, ".")

	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		l, err1 := strconv.Atoi(latestParts[i])
		c, err2 := strconv.Atoi(currentParts[i])
		if err1 != nil || err2 != nil {
			if latestParts[i] > currentParts[i] {
				return true
			}
			if latestParts[i] < currentParts[i] {
				return false
			}
			continue
		}
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}

	if len(latestParts) > len(currentParts) {
		return true
	}
	if len(latestParts) < len(currentParts) {
		return false
	}
	if currentIsDev && !latestIsDev {
		return true
	}
	return false
}
