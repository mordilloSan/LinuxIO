package apischema

import (
	"reflect"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
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

// enumNames returns the values of a string type as plain strings.
func enumNames[T ~string](values ...T) []string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = string(value)
	}
	return names
}

// StringEnums gives the generator the values of each named string type it
// renders as a literal union. An entry takes its values from the Go definition
// they describe, where this package can import it: the UI choices from the
// lists in userconfig, the rest from their constants. CapabilityName is added
// by handlers/capabilities from its registry when that package is loaded.
// AutoUpdateBackend, AutoUpdateNotify, VMImagePresetID and VMFirmware are
// defined in the handlers, which import this package, so their entries are
// retyped here and a test beside each definition compares the two.
var StringEnums = map[string][]string{
	"AutoUpdateFrequency":           {"hourly", "daily", "weekly"},
	"AutoUpdateScope":               {"security", "updates", "all"},
	"AutoUpdateRebootPolicy":        {"never", "if_needed", "always", "schedule"},
	"AutoUpdateNotify":              {"never", "on_failure", "on_change", "always"},
	"AutoUpdateBackend":             {"apt-unattended", "mintupdate-automation", "dnf-automatic", "dnf5-automatic"},
	"DockerContainerAutoUpdateMode": {"update", "check_only"},
	"DockerUpdateNotify":            {"never", "on_failure", "on_change"},
	"DockerUpdateCheckState":        {"current", "available", "uncheckable", "error"},
	"ContainerMetricsStatus":        enumNames(ContainerMetricsStatusAvailable, ContainerMetricsStatusStale, ContainerMetricsStatusUnavailable, ContainerMetricsStatusNotRunning),
	"ConfigStorageMode":             enumNames(userconfig.StorageModeHome, userconfig.StorageModeFallback, userconfig.StorageModeMemory),
	"FileUploadOwner":               {"session"},
	"TaskState":                     enumNames(bridgeipc.TaskStateQueued, bridgeipc.TaskStateRunning, bridgeipc.TaskStateCompleted, bridgeipc.TaskStateFailed, bridgeipc.TaskStateCanceled, bridgeipc.TaskStateInterrupted),
	"MonitoringHistoryResolution":   monitoringapi.HistoryResolutions(),
	// SensorReadingKind is defined by the shared monitoring API; keeping its
	// generator metadata here preserves the public literal union.
	"SensorReadingKind":         {"number", "boolean"},
	"TableCardViewMode":         userconfig.ViewModes,
	"ThemeColorKey":             userconfig.ThemeColorKeys(),
	"Theme":                     enumNames(userconfig.Themes...),
	"NavigationMode":            userconfig.NavigationModes,
	"DockTileColors":            userconfig.DockTileColorModes,
	"ValidationIssueType":       {"error", "warning"},
	"VMImagePresetID":           {"home-assistant-os", "debian-server", "ubuntu-server", "fedora-cloud"},
	"VMSourceType":              {"iso", "imagePreset", "import"},
	"VMFirmware":                {"bios", "uefi"},
	"VMCPUMode":                 {"host-passthrough", "host-model", "default", "custom"},
	"NetworkBridgeHandoffState": enumNames(NetworkBridgeHandoffApplying, NetworkBridgeHandoffAwaitingConfirmation, NetworkBridgeHandoffConfirmed, NetworkBridgeHandoffReverted, NetworkBridgeHandoffUnknown),
	"WireGuardHookKind":         enumNames(WireGuardHookKinds...),
}

var ExtraTypes = []TypeSpec{
	TypeOf[InstallCapabilityResult](),
	TypeOf[TaskEvent](),
	TypeOf[VMCreateProgress](),
}

type TaskProgress = bridgeipc.TaskProgress
