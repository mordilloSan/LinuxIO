package app

import "time"

// reseedMinWindow is the smallest delta window a reseeded baseline may leave.
// Adopting a collector baseline that is only milliseconds old would hand the
// request a window of milliseconds, which reads as zero CPU or as a rate
// amplified by 1000/δ.
const reseedMinWindow = time.Second

// shouldReseedFromCollector reports whether a live key should adopt the
// collector's baseline: the collector's baseline must be newer than the live
// key's (a zero liveAt means the key has no baseline) and at least
// reseedMinWindow old at request time.
func shouldReseedFromCollector(liveAt, collectorAt, now time.Time) bool {
	if collectorAt.IsZero() {
		return false
	}
	if !liveAt.IsZero() && !liveAt.Before(collectorAt) {
		return false
	}
	return now.Sub(collectorAt) >= reseedMinWindow
}
