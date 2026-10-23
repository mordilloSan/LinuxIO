package sensors

import (
	psutilSensors "github.com/shirou/gopsutil/v4/sensors"
)

var getSensorTemps = psutilSensors.TemperaturesWithContext
