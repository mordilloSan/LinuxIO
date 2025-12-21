package updates

// Types for update history (server-side log parsing)

type UpgradeItem struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
}

type UpdateHistoryEntry struct {
	Date     string        `json:"date"`
	Upgrades []UpgradeItem `json:"upgrades"`
}
