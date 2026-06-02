package autoupdate

import "fmt"

func onCalendarFor(freq string) (string, error) {
	switch freq {
	case "hourly":
		return "hourly", nil
	case "daily":
		return "daily", nil
	case "weekly":
		return "weekly", nil
	default:
		return "", fmt.Errorf("invalid frequency: %s", freq)
	}
}
