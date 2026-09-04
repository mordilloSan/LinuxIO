// Package deltatracker provides a tracker for calculating differences in numeric values over time.
package deltatracker

import (
	"maps"
	"sync"
)

// Numeric is a constraint that permits any integer or floating-point type.
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// DeltaTracker is a generic, thread-safe tracker for calculating differences
// in numeric values over time.
// K is the key type (e.g., int, string).
// V is the value type (e.g., int, int64, float32, float64).
type DeltaTracker[K comparable, V Numeric] struct {
	sync.RWMutex
	current  map[K]V
	previous map[K]V
}

// NewDeltaTracker creates a new generic tracker.
func NewDeltaTracker[K comparable, V Numeric]() *DeltaTracker[K, V] {
	return &DeltaTracker[K, V]{
		current:  make(map[K]V),
		previous: make(map[K]V),
	}
}

// Set records the current value for a given ID.
func (t *DeltaTracker[K, V]) Set(id K, value V) {
	t.Lock()
	defer t.Unlock()
	t.current[id] = value
}

// Deltas returns a map of all calculated deltas for the current interval.
func (t *DeltaTracker[K, V]) Deltas() map[K]V {
	t.RLock()
	defer t.RUnlock()

	deltas := make(map[K]V)
	for id, currentVal := range t.current {
		if previousVal, ok := t.previous[id]; ok {
			deltas[id] = currentVal - previousVal
		} else {
			deltas[id] = 0
		}
	}
	return deltas
}

// Previous returns the previously recorded value for the given key, if it exists.
func (t *DeltaTracker[K, V]) Previous(id K) (V, bool) {
	t.RLock()
	defer t.RUnlock()

	value, ok := t.previous[id]
	return value, ok
}

// Delta returns the delta for a single key.
// Returns 0 if the key doesn't exist or has no previous value.
func (t *DeltaTracker[K, V]) Delta(id K) V {
	t.RLock()
	defer t.RUnlock()

	currentVal, currentOk := t.current[id]
	if !currentOk {
		return 0
	}

	previousVal, previousOk := t.previous[id]
	if !previousOk {
		return 0
	}

	return currentVal - previousVal
}

// Cycle prepares the tracker for the next interval.
func (t *DeltaTracker[K, V]) Cycle() {
	t.Lock()
	defer t.Unlock()
	t.previous = t.current
	t.current = make(map[K]V)
}

// Clone returns an independent copy of both generations.
func (t *DeltaTracker[K, V]) Clone() *DeltaTracker[K, V] {
	t.RLock()
	defer t.RUnlock()
	return &DeltaTracker[K, V]{current: maps.Clone(t.current), previous: maps.Clone(t.previous)}
}
