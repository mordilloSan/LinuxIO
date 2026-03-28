package shares

// NFSExport represents an entry in /etc/exports (server-side NFS share)
type NFSExport struct {
	Path    string      `json:"path"`    // Shared directory path
	Clients []NFSClient `json:"clients"` // Client access rules
	Active  bool        `json:"active"`  // Currently exported (from exportfs -v)
}

// NFSClient represents a client access rule for an NFS export
type NFSClient struct {
	Host    string   `json:"host"`    // Hostname, IP, network CIDR, or "*" for everyone
	Options []string `json:"options"` // Export options (rw, sync, no_subtree_check, etc.)
}

// SambaShare represents a share section in smb.conf
type SambaShare struct {
	Name       string            `json:"name"`       // Share name ([section] header)
	Properties map[string]string `json:"properties"` // All key=value properties
}

// NFSConnectedClient represents a client currently connected to an NFS export
type NFSConnectedClient struct {
	IP        string `json:"ip"`        // Client IP address
	ExportPath string `json:"exportPath"` // Mounted export path
}

// SambaConnectedClient represents a client currently connected to a Samba share
type SambaConnectedClient struct {
	Username string `json:"username"` // Connected user
	IP       string `json:"ip"`       // Client IP address
	Share    string `json:"share"`    // Share name
}
