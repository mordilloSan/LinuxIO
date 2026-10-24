package api

import systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"

// SystemSummary is the compact host payload of the system summary route.
type SystemSummary = systemmodel.Summary

// SystemSummaryResponse is the body of the system summary route.
type SystemSummaryResponse struct {
	CapturedAt int64 `json:"captured_at"`
	SystemSummary
}
