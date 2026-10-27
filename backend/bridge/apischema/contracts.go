package apischema

import (
	"reflect"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

// TypeSpec is a route payload type reflected into generated TypeScript.
type TypeSpec struct {
	GoType reflect.Type
}

// NoRequest marks a route that takes no request payload.
type NoRequest struct{}

// NoResponse marks a route that returns no result payload.
type NoResponse struct{}

func TypeOf[T any]() TypeSpec {
	return TypeSpec{GoType: reflect.TypeFor[T]()}
}

var (
	noRequestType  = reflect.TypeFor[NoRequest]()
	noResponseType = reflect.TypeFor[NoResponse]()
)

// IsVoidType reports whether t is the NoResponse contract, through any level of
// pointer. This is the single definition of "carries no result": the bridge emit
// path uses it to keep nil on the wire, and the TypeScript generator uses it to
// render `void`. They must agree, so neither gets its own copy.
func IsVoidType(t reflect.Type) bool {
	return derefType(t) == noResponseType
}

// IsEmptyRequestType reports whether t is the NoRequest contract, through any
// level of pointer.
func IsEmptyRequestType(t reflect.Type) bool {
	return derefType(t) == noRequestType
}

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// Void reports whether this payload carries no result. Reads the reflect.Type
// the route already materialized at construction — no extra reflection.
func (t TypeSpec) Void() bool {
	return IsVoidType(t.GoType)
}

// Common route contract fragments.
type MessageResponse struct {
	Message string `json:"message"`
}

type SuccessResponse struct {
	Success bool `json:"success"`
}

type SuccessPathResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
}

type SuccessNameResponse struct {
	Success bool   `json:"success"`
	Name    string `json:"name"`
}

type NameRequest struct {
	Name string `json:"name"`
}

type PathRequest struct {
	Path string `json:"path"`
}

var StringEnums = map[string][]string{
	"AutoUpdateFrequency":           {"hourly", "daily", "weekly"},
	"AutoUpdateScope":               {"security", "updates", "all"},
	"AutoUpdateRebootPolicy":        {"never", "if_needed", "always", "schedule"},
	"AutoUpdateNotify":              {"never", "on_failure", "on_change", "always"},
	"AutoUpdateBackend":             {"apt-unattended", "mintupdate-automation", "dnf-automatic", "dnf5-automatic"},
	"DockerContainerAutoUpdateMode": {"update", "check_only"},
	"DockerUpdateCheckState":        {"current", "available", "uncheckable", "error"},
	"ContainerMetricsStatus":        {"available", "stale", "unavailable", "not_running"},
	"ConfigStorageMode":             {"home", "fallback", "memory"},
	"TaskState":                     {"queued", "running", "completed", "failed", "canceled", "interrupted"},
	"MonitoringHistoryResolution":   monitoringapi.HistoryResolutions(),
	// SensorReadingKind is defined by the shared monitoring API; keeping its
	// generator metadata here preserves the public literal union.
	"SensorReadingKind":         {"number", "boolean"},
	"TableCardViewMode":         {"card", "table"},
	"Theme":                     {"LIGHT", "DARK"},
	"NavigationMode":            {"sidebar", "dock"},
	"DockTileColors":            {"accent", "mono", "neutral", "vibrant"},
	"ValidationIssueType":       {"error", "warning"},
	"VMImagePresetID":           {"home-assistant-os", "debian-server", "ubuntu-server", "fedora-cloud"},
	"VMSourceType":              {"iso", "imagePreset", "import"},
	"VMFirmware":                {"bios", "uefi"},
	"NetworkBridgeHandoffState": {"applying", "awaiting_confirmation", "confirmed", "reverted", "unknown"},
}

var ExtraTypes = []TypeSpec{
	TypeOf[InstallCapabilityResult](),
	TypeOf[TaskEvent](),
	TypeOf[VMCreateProgress](),
}

type TaskProgress = bridgeipc.TaskProgress
