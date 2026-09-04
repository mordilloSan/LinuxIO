package app

import (
	"github.com/shirou/gopsutil/v4/sensors"
)

var getSensorTemps = sensors.TemperaturesWithContext
