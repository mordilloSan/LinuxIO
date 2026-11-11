package alerts

import (
	"fmt"
	"time"
)

// RanFor says in an alert's message how long a run took, rounded down: "It ran
// for 12 s.", "It ran for 5 min.", "It ran for 3 h 12 min.", and "It ran for
// less than 1 s." for a run shorter than a second.
func RanFor(d time.Duration) string {
	hours, minutes := int64(d/time.Hour), int64(d%time.Hour/time.Minute)
	switch {
	case d < time.Second:
		return "It ran for less than 1 s."
	case d < time.Minute:
		return fmt.Sprintf("It ran for %d s.", int64(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("It ran for %d min.", minutes)
	case minutes == 0:
		return fmt.Sprintf("It ran for %d h.", hours)
	}
	return fmt.Sprintf("It ran for %d h %d min.", hours, minutes)
}
