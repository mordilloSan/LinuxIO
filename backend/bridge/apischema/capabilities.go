package apischema

// CapabilityName names one optional host capability. The names are the
// capability registry's, which handlers/capabilities publishes as the
// StringEnums union when it is loaded, so the registry is the one list.
type CapabilityName string

// CapabilityStatus is one capability's detection result. Error is the reason
// detection gave, when it gave one.
type CapabilityStatus struct {
	Available bool    `json:"available"`
	Error     *string `json:"error,omitzero"`
}

// CapabilitiesResponse holds the status of every registered capability by
// name.
type CapabilitiesResponse map[CapabilityName]CapabilityStatus
