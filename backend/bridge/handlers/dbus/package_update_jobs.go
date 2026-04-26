package dbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	godbus "github.com/godbus/dbus/v5"

	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
)

const JobTypePackageUpdate = "package.update"

func RegisterJobRunners() {
	bridgejobs.RegisterRunner(JobTypePackageUpdate, runPackageUpdateJob)
}

// PkgUpdateProgress represents progress for package update operations.
type PkgUpdateProgress struct {
	Type           string  `json:"type"`                      // "item_progress", "package", "status", "percentage", "message"
	PackageID      string  `json:"package_id,omitempty"`      // Current package being processed
	PackageSummary string  `json:"package_summary,omitempty"` // Package summary from Package(...) signal
	Status         string  `json:"status,omitempty"`          // Status description (e.g., "Downloading", "Installing")
	Message        string  `json:"message,omitempty"`         // Rich backend message text when available
	StatusCode     *uint32 `json:"status_code,omitempty"`     // PackageKit status enum
	InfoCode       *uint32 `json:"info_code,omitempty"`       // PackageKit info enum (Package signal)
	Percentage     *uint32 `json:"percentage,omitempty"`      // Overall percentage (0-100, 101=unknown)
	ItemPct        *uint32 `json:"item_pct,omitempty"`        // Per-item percentage for ItemProgress
}

type pkgUpdateReporter func(*PkgUpdateProgress) error

func jobPkgUpdateReporter(job *bridgejobs.Job) pkgUpdateReporter {
	return func(p *PkgUpdateProgress) error {
		job.ReportProgress(*p)
		return nil
	}
}

func reportPkgUpdateProgress(report pkgUpdateReporter, p *PkgUpdateProgress) {
	if err := report(p); err != nil {
		slog.Debug("failed to write progress frame", "component", "dbus", "subsystem", "packagekit", "error", err)
	}
}

// PackageKit status enum values (from org.freedesktop.PackageKit documentation)
var pkStatusNames = map[uint32]string{
	0:  "Unknown",
	1:  "Wait",
	2:  "Setup",
	3:  "Running",
	4:  "Query",
	5:  "Info",
	6:  "Remove",
	7:  "Refresh cache",
	8:  "Download",
	9:  "Install",
	10: "Update",
	11: "Cleanup",
	12: "Obsolete",
	13: "Dep resolve",
	14: "Sig check",
	15: "Test commit",
	16: "Commit",
	17: "Request",
	18: "Finished",
	19: "Cancel",
	20: "Download repository",
	21: "Download packagelist",
	22: "Download filelist",
	23: "Download changelog",
	24: "Download group",
	25: "Download updateinfo",
	26: "Repackaging",
	27: "Loading cache",
	28: "Scan applications",
	29: "Generate package list",
	30: "Waiting for lock",
	31: "Waiting for auth",
	32: "Scan process list",
	33: "Check executable files",
	34: "Check libraries",
	35: "Copy files",
	36: "Run hook",
}

// PackageKit package info enum values for Package(...) signal.
// We only map update-relevant phases and fall back to "Package event <code>".
var pkInfoNames = map[uint32]string{
	10: "Downloading",
	11: "Updating",
	12: "Installing",
	13: "Removing",
	14: "Cleanup",
	15: "Obsoleting",
	19: "Reinstalling",
	20: "Downgrading",
	21: "Preparing",
}

func packageInfoName(info uint32) string {
	if n, ok := pkInfoNames[info]; ok {
		return n
	}
	return fmt.Sprintf("Package event %d", info)
}

// Status codes that represent actual package work (should update progress bar)
var realWorkStatuses = map[uint32]bool{
	8:  true, // Download (actual package download)
	9:  true, // Install
	10: true, // Update
	11: true, // Cleanup
	13: true, // Dep resolve
	14: true, // Sig check
	15: true, // Test commit
	16: true, // Commit
	35: true, // Copy files
}

// isRealWorkStatus returns true if this status represents actual package work
func isRealWorkStatus(status uint32) bool {
	return realWorkStatuses[status]
}

func runPackageUpdateJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	if len(args) == 0 {
		return nil, bridgejobs.NewError("no packages specified", 400)
	}
	report := jobPkgUpdateReporter(job)
	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "status",
		Status:     "Initializing",
		Percentage: new(uint32(0)),
	})

	if err := updatePackagesWithProgress(ctx, args, report); err != nil {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		return nil, bridgejobs.NewError(err.Error(), 500)
	}

	result := map[string]any{"updated": len(args)}
	return result, nil
}

func updatePackagesWithProgress(ctx context.Context, packageIDs []string, report pkgUpdateReporter) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			slog.Warn("failed to close D-Bus connection", "component", "dbus", "subsystem", "packagekit", "error", cerr)
		}
	}()

	const (
		pkBusName      = "org.freedesktop.PackageKit"
		pkObjPath      = "/org/freedesktop/PackageKit"
		transactionIfc = "org.freedesktop.PackageKit.Transaction"
	)

	trans, transPath, err := createPackageKitTransaction(conn, pkBusName, pkObjPath)
	if err != nil {
		return err
	}
	sigCh := subscribePackageKitSignals(conn, transPath, 100)
	defer conn.RemoveSignal(sigCh)
	defer removePackageKitSignalMatch(conn, transPath)
	// Call UpdatePackages with all package IDs at once
	// Flag 0 = no special flags (install normally)
	slog.Info("calling PackageKit UpdatePackages", "component", "dbus", "subsystem", "packagekit", "error", fmt.Errorf("packages=%d", len(packageIDs)))
	call := trans.Call(transactionIfc+".UpdatePackages", 0, uint64(0), packageIDs)
	if call.Err != nil {
		return fmt.Errorf("UpdatePackages failed: %w", call.Err)
	}

	// Process signals until Finished or error
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute) // Long timeout for large updates
	defer cancel()
	var lastWorkStatus uint32

	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				continue
			}
			done, err := handlePackageUpdateSignal(report, sig, transactionIfc, &lastWorkStatus)
			if err != nil {
				return err
			}
			if done {
				return nil
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for package updates to complete")
		}
	}
}

func createPackageKitTransaction(
	conn *godbus.Conn,
	busName, objectPath string,
) (godbus.BusObject, godbus.ObjectPath, error) {
	obj := conn.Object(busName, godbus.ObjectPath(objectPath))
	var transPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return nil, "", fmt.Errorf("CreateTransaction failed: %w", err)
	}
	return conn.Object(busName, transPath), transPath, nil
}

func subscribePackageKitSignals(
	conn *godbus.Conn,
	transPath godbus.ObjectPath,
	buffer int,
) chan *godbus.Signal {
	sigCh := make(chan *godbus.Signal, buffer)
	conn.Signal(sigCh)
	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		slog.Warn("failed to add D-Bus match signal", "component", "dbus", "subsystem", "packagekit", "error", err)
	}
	return sigCh
}

func removePackageKitSignalMatch(conn *godbus.Conn, transPath godbus.ObjectPath) {
	if err := conn.RemoveMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		slog.Debug("failed to remove D-Bus match signal", "component", "dbus", "subsystem", "packagekit", "error", err)
	}
}

func handlePackageUpdateSignal(
	report pkgUpdateReporter,
	sig *godbus.Signal,
	transactionIfc string,
	lastWorkStatus *uint32,
) (bool, error) {
	switch sig.Name {
	case transactionIfc + ".ItemProgress":
		handleItemProgressSignal(report, sig, lastWorkStatus)
	case transactionIfc + ".Package":
		handlePackageSignal(report, sig)
	case transactionIfc + ".Message":
		handleMessageSignal(report, sig)
	case transactionIfc + ".Percentage":
		return false, nil
	case transactionIfc + ".ErrorCode":
		return false, packageUpdateSignalError(sig)
	case transactionIfc + ".Finished":
		handleFinishedSignal(report)
		return true, nil
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		handlePropertiesChangedSignal(report, sig, transactionIfc, lastWorkStatus)
	}
	return false, nil
}

func handleItemProgressSignal(report pkgUpdateReporter, sig *godbus.Signal, lastWorkStatus *uint32) {
	if len(sig.Body) < 3 {
		return
	}
	pkgID, _ := sig.Body[0].(string)
	status, _ := sig.Body[1].(uint32)
	pct, _ := sig.Body[2].(uint32)
	if !isRealWorkStatus(status) {
		return
	}
	*lastWorkStatus = status

	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "item_progress",
		PackageID:  pkgID,
		Status:     packageStatusName(status),
		StatusCode: new(status),
		ItemPct:    new(pct),
	})
}

func handlePackageSignal(report pkgUpdateReporter, sig *godbus.Signal) {
	if len(sig.Body) < 3 {
		return
	}
	info, _ := sig.Body[0].(uint32)
	pkgID, _ := sig.Body[1].(string)
	summary, _ := sig.Body[2].(string)

	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:           "package",
		PackageID:      pkgID,
		PackageSummary: summary,
		Status:         packageInfoName(info),
		InfoCode:       new(info),
	})
}

func handleMessageSignal(report pkgUpdateReporter, sig *godbus.Signal) {
	if len(sig.Body) < 2 {
		return
	}
	msgType, _ := sig.Body[0].(uint32)
	details, _ := sig.Body[1].(string)

	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:    "message",
		Status:  fmt.Sprintf("Message %d", msgType),
		Message: details,
	})
}

func packageUpdateSignalError(sig *godbus.Signal) error {
	if len(sig.Body) >= 2 {
		code, _ := sig.Body[0].(uint32)
		details, _ := sig.Body[1].(string)
		return fmt.Errorf("PackageKit error %d: %s", code, details)
	}
	return fmt.Errorf("PackageKit error (unknown)")
}

func handleFinishedSignal(report pkgUpdateReporter) {
	slog.Info("Finished signal received")
	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "status",
		Status:     "Finished",
		Percentage: new(uint32(100)),
	})
}

func handlePropertiesChangedSignal(
	report pkgUpdateReporter,
	sig *godbus.Signal,
	transactionIfc string,
	lastWorkStatus *uint32,
) {
	props, statusForPercentage, currentStatus, hasStatus := parseTransactionProperties(sig, transactionIfc, *lastWorkStatus)
	if props == nil {
		return
	}
	if hasStatus && isRealWorkStatus(currentStatus) {
		*lastWorkStatus = currentStatus
	}
	writePercentageProgress(report, props, statusForPercentage)
	writeStatusProgress(report, currentStatus, hasStatus)
}

func parseTransactionProperties(
	sig *godbus.Signal,
	transactionIfc string,
	lastWorkStatus uint32,
) (map[string]godbus.Variant, uint32, uint32, bool) {
	if len(sig.Body) < 2 {
		return nil, 0, 0, false
	}
	iface, _ := sig.Body[0].(string)
	if iface != transactionIfc {
		return nil, 0, 0, false
	}
	props, ok := sig.Body[1].(map[string]godbus.Variant)
	if !ok {
		return nil, 0, 0, false
	}

	currentStatus, hasStatus := propertyUint32(props, "Status")
	statusForPercentage := currentStatus
	if statusForPercentage == 0 {
		statusForPercentage = lastWorkStatus
	}
	return props, statusForPercentage, currentStatus, hasStatus
}

func writePercentageProgress(report pkgUpdateReporter, props map[string]godbus.Variant, status uint32) {
	pct, ok := propertyUint32(props, "Percentage")
	if !ok || !isRealWorkStatus(status) {
		return
	}
	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "percentage",
		Percentage: new(pct),
	})
}

func writeStatusProgress(report pkgUpdateReporter, currentStatus uint32, hasStatus bool) {
	if !hasStatus || currentStatus == 0 || !isRealWorkStatus(currentStatus) {
		return
	}
	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "status",
		Status:     packageStatusName(currentStatus),
		StatusCode: new(currentStatus),
	})
}

func propertyUint32(props map[string]godbus.Variant, key string) (uint32, bool) {
	variant, ok := props[key]
	if !ok {
		return 0, false
	}
	value, ok := variant.Value().(uint32)
	return value, ok
}

func packageStatusName(status uint32) string {
	statusName := pkStatusNames[status]
	if statusName == "" {
		return fmt.Sprintf("Status %d", status)
	}
	return statusName
}
