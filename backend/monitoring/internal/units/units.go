// Package units converts and rounds the measurements the collectors report.
package units

import (
	"math"
)

// BytesToMegabytes converts bytes to megabytes and rounds to two decimal places.
func BytesToMegabytes(b float64) float64 {
	return TwoDecimals(b / 1048576)
}

// BytesToGigabytes converts bytes to gigabytes and rounds to two decimal places.
func BytesToGigabytes(b uint64) float64 {
	return TwoDecimals(float64(b) / 1073741824)
}

// TwoDecimals rounds a float64 value to two decimal places.
func TwoDecimals(value float64) float64 {
	return math.Round(value*100) / 100
}
