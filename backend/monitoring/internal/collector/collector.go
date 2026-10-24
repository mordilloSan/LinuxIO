// Package collector holds what every collector shares: the key of the
// periodic collector tick, the policy for reseeding a live key's delta
// baseline from that tick, and the single-flight Flight that live requests
// join. The collectors themselves live in the sub-packages.
package collector

// TickKey is the delta-state key of the periodic collector pass. It isolates
// the collector's baselines from live API samples; the configured ticker
// controls the cadence.
const TickKey uint16 = 60_000
