package apischema

// VersionResponse is the running LinuxIO build and, when PackageKit lists
// one, the version of the pending update to the linuxio package, as
// PackageKit gives it (0.56.0 from a DEB, 0.56.0-1 from an RPM).
type VersionResponse struct {
	CurrentVersion string `json:"current_version"`
	UpdateVersion  string `json:"update_version,omitempty"`
}
