package apischema

type AutoUpdateFrequency string
type AutoUpdateScope string
type AutoUpdateRebootPolicy string
type AutoUpdateBackend string
type AutoUpdateNotify string

type AutoUpdateOptions struct {
	DownloadOnly    bool                   `json:"download_only"`
	Enabled         bool                   `json:"enabled"`
	ExcludePackages []string               `json:"exclude_packages"`
	ExtraOrigins    []string               `json:"extra_origins"`
	Frequency       AutoUpdateFrequency    `json:"frequency"`
	Notify          AutoUpdateNotify       `json:"notify"`
	RebootPolicy    AutoUpdateRebootPolicy `json:"reboot_policy"`
	Scope           AutoUpdateScope        `json:"scope"`
}

type AutoUpdateOptionSupport struct {
	DownloadOnly    bool                     `json:"download_only"`
	ExcludePackages bool                     `json:"exclude_packages"`
	ExtraOrigins    bool                     `json:"extra_origins"`
	Frequencies     []AutoUpdateFrequency    `json:"frequencies"`
	NotifyPolicies  []AutoUpdateNotify       `json:"notify_policies"`
	RebootPolicies  []AutoUpdateRebootPolicy `json:"reboot_policies"`
	Scopes          []AutoUpdateScope        `json:"scopes"`
}

// AutoUpdateOrigin is a package repository that can be opted into automatic
// updates on top of the distribution scope.
type AutoUpdateOrigin struct {
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
}

type AutoUpdateState struct {
	AvailableOrigins []AutoUpdateOrigin      `json:"available_origins,omitempty"`
	Backend          AutoUpdateBackend       `json:"backend"`
	CanConfigure     bool                    `json:"can_configure"`
	Notes            []string                `json:"notes,omitempty"`
	Options          AutoUpdateOptions       `json:"options"`
	Support          AutoUpdateOptionSupport `json:"support"`
}

type UpgradeItem struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
}

type UpdateHistoryRow struct {
	Date     string        `json:"date"`
	Upgrades []UpgradeItem `json:"upgrades"`
}

type InstallCapabilityResult struct {
	Available bool    `json:"available"`
	Error     *string `json:"error,omitempty"`
	Warning   *string `json:"warning,omitempty"`
}

type Update struct {
	Changelog string   `json:"changelog"`
	CVE       []string `json:"cve"`
	InfoEnum  uint32   `json:"info_enum"`
	Issued    string   `json:"issued"`
	PackageID string   `json:"package_id"`
	Restart   int      `json:"restart"`
	State     int      `json:"state"`
	Summary   string   `json:"summary"`
	Version   string   `json:"version"`
}

type OfflineUpdatesResponse struct {
	Status *string `json:"status,omitempty"`
	Error  *string `json:"error,omitempty"`
}

type PackageUpdateRequest struct {
	PackageIDs []string `json:"packageIds"`
}

type CapabilityRequest struct {
	Capability string `json:"capability"`
}

type PackageIDRequest struct {
	PackageID string `json:"packageId"`
}
