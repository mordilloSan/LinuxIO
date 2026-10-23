package session

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type User struct {
	Username string `json:"username"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
}

type Timing struct {
	CreatedAt     time.Time `json:"created_at"`
	LastAccess    time.Time `json:"last_access"`
	LastRefresh   time.Time `json:"last_refresh"`
	IdleUntil     time.Time `json:"idle_until"`
	AbsoluteUntil time.Time `json:"absolute_until"`
}

type CapabilitiesAvailable struct {
	DockerAvailable           bool `json:"docker_available"`
	DockerUpdatesAvailable    bool `json:"docker_updates_available"`
	MonitoringAvailable       bool `json:"monitoring_available"`
	LMSensorsAvailable        bool `json:"lm_sensors_available"`
	MemoryInventoryAvailable  bool `json:"memory_inventory_available"`
	SmartmontoolsAvailable    bool `json:"smartmontools_available"`
	PackageKitAvailable       bool `json:"packagekit_available"`
	NFSClientAvailable        bool `json:"nfs_client_available"`
	NFSServerAvailable        bool `json:"nfs_server_available"`
	SambaServerAvailable      bool `json:"samba_server_available"`
	SambaClientAvailable      bool `json:"samba_client_available"`
	RsyncAvailable            bool `json:"rsync_available"`
	TunedAvailable            bool `json:"tuned_available"`
	AvahiAvailable            bool `json:"avahi_available"`
	WireGuardAvailable        bool `json:"wireguard_available"`
	LibvirtAvailable          bool `json:"libvirt_available"`
	NodeAvailable             bool `json:"node_available"`
	BtrfsMaintenanceAvailable bool `json:"btrfs_maintenance_available"`
	BtrfsAvailable            bool `json:"btrfs_available"`
}

type CapabilitiesError struct {
	DockerError           *string `json:"docker_error,omitempty"`
	DockerUpdatesError    *string `json:"docker_updates_error,omitempty"`
	MonitoringError       *string `json:"monitoring_error,omitempty"`
	LMSensorsError        *string `json:"lm_sensors_error,omitempty"`
	MemoryInventoryError  *string `json:"memory_inventory_error,omitempty"`
	SmartmontoolsError    *string `json:"smartmontools_error,omitempty"`
	PackageKitError       *string `json:"packagekit_error,omitempty"`
	NFSClientError        *string `json:"nfs_client_error,omitempty"`
	NFSServerError        *string `json:"nfs_server_error,omitempty"`
	SambaServerError      *string `json:"samba_server_error,omitempty"`
	SambaClientError      *string `json:"samba_client_error,omitempty"`
	RsyncError            *string `json:"rsync_error,omitempty"`
	TunedError            *string `json:"tuned_error,omitempty"`
	AvahiError            *string `json:"avahi_error,omitempty"`
	WireGuardError        *string `json:"wireguard_error,omitempty"`
	LibvirtError          *string `json:"libvirt_error,omitempty"`
	NodeError             *string `json:"node_error,omitempty"`
	BtrfsMaintenanceError *string `json:"btrfs_maintenance_error,omitempty"`
	BtrfsError            *string `json:"btrfs_error,omitempty"`
}

// Session is the cross-process session record shared by the webserver, the
// auth daemon and the bridge. The webserver-only manager lives in
// backend/webserver/sessions.
type Session struct {
	SessionID    string                `json:"session_id"`
	User         User                  `json:"user"`
	Privileged   bool                  `json:"privileged"`
	Capabilities CapabilitiesAvailable `json:"capabilities"`
	Timing       Timing                `json:"timing"`
}

// DiagnosticRef returns a stable, non-secret correlation value for a session
// credential. Production session IDs contain 128 bits of randomness, so a
// one-way digest preserves diagnostic correlation without disclosing the
// bearer credential itself. The result is diagnostic-only and must never be
// accepted for authentication or authorization.
func DiagnosticRef(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("linuxio-session-diagnostic-ref\x00" + sessionID))
	return "sr-" + hex.EncodeToString(sum[:16])
}
