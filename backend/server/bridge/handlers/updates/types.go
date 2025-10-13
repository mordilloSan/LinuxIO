package updates

type UpdateGroup struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Severity string   `json:"severity"`
	Packages []string `json:"packages"`
}

type UpgradeItem struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
}

type UpdateHistoryEntry struct {
	Date     string        `json:"date"`
	Upgrades []UpgradeItem `json:"upgrades"`
}

type Update struct {
	PackageID string   `json:"package_id"`
	Summary   string   `json:"summary"`
	Version   string   `json:"version"`
	Issued    string   `json:"issued"`
	Changelog string   `json:"changelog"`
	CVEs      []string `json:"cve"`
	Restart   uint32   `json:"restart"`
	State     uint32   `json:"state"`
}

type AutoUpdateOptions struct {
	Enabled      bool     `json:"enabled"`
	Frequency    string   `json:"frequency"`     // "hourly"|"daily"|"weekly"
	Scope        string   `json:"scope"`         // "security"|"updates"|"all"
	DownloadOnly bool     `json:"download_only"` // download but don’t auto-install
	RebootPolicy string   `json:"reboot_policy"` // "never"|"if_needed"|"always"|"schedule"
	ExcludePkgs  []string `json:"exclude_packages"`
}
