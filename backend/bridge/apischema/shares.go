package apischema

type NFSExport struct {
	Active  bool        `json:"active"`
	Clients []NFSClient `json:"clients"`
	Path    string      `json:"path"`
}

type SambaShare struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type NFSClient struct {
	Host    string   `json:"host"`
	Options []string `json:"options"`
}

type ShareNFSRequest struct {
	Path    string      `json:"path"`
	Clients []NFSClient `json:"clients"`
}

type ShareSambaRequest struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type ShareUpdateSambaRequest struct {
	OldName    string            `json:"oldName"`
	NewName    string            `json:"newName"`
	Properties map[string]string `json:"properties"`
}
