package packages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var packageUpdateRoutes = packageUpdateBindings().Routes()

func packageUpdateBindings() apischema.BindingSet {
	policy := bridgejobs.SingletonSystem
	policy.Timeout = 2 * time.Hour
	return apischema.Bindings(
		apischema.Runner[apischema.PackageUpdateRequest, apischema.JobSnapshot]("packages.update", apischema.WithJobMetadata(func(req apischema.PackageUpdateRequest) bridgejobs.JobMetadata {
			return bridgejobs.JobMetadata{Identity: append([]string{}, req.PackageIDs...), Label: "Updating packages", PackageIDs: append([]string{}, req.PackageIDs...)}
		})).Run(runPackageUpdateJob, policy),
	)
}

func RegisterJobRoutes(router *bridgejobs.Router) {
	packageUpdateBindings().Register(router)
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
var pkInfoNames = map[uint32]string{
	1:  "Installed",
	2:  "Available",
	3:  "Low priority",
	4:  "Enhancement",
	5:  "Normal priority",
	6:  "Bug fix",
	7:  "Important",
	8:  "Security",
	9:  "Blocked",
	10: "Downloading",
	11: "Updating",
	12: "Installing",
	13: "Removing",
	14: "Cleanup",
	15: "Obsoleting",
	16: "Collection installed",
	17: "Collection available",
	18: "Finished",
	19: "Reinstalling",
	20: "Downgrading",
	21: "Preparing",
	22: "Decompressing",
	23: "Untrusted",
	24: "Trusted",
	25: "Unavailable",
	26: "Critical",
	27: "Install",
	28: "Remove",
	29: "Obsolete",
	30: "Downgrade",
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

func runPackageUpdateJob(ctx context.Context, job *bridgejobs.Job, req apischema.PackageUpdateRequest) (any, error) {
	if len(req.PackageIDs) == 0 {
		return nil, bridgejobs.NewError("no packages specified", 400)
	}
	report := jobPkgUpdateReporter(job)
	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "status",
		Status:     "Initializing",
		Percentage: new(uint32(0)),
	})

	if err := updatePackagesWithProgress(ctx, req.PackageIDs, report); err != nil {
		if ctx.Err() != nil {
			return nil, context.Canceled
		}
		return nil, bridgejobs.NewError(err.Error(), 500)
	}

	result := map[string]any{"updated": len(req.PackageIDs)}
	return result, nil
}

func updatePackagesWithProgress(ctx context.Context, packageIDs []string, report pkgUpdateReporter) error {
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		return updatePackageBatch(session, packageIDs, report)
	})
}

func updatePackageBatch(session pkgkit.ClientSession, packageIDs []string, report pkgUpdateReporter) error {
	failures := make([]string, 0)
	var firstFailure error
	updated := 0
	for index, packageID := range packageIDs {
		if err := session.Context().Err(); err != nil {
			return err
		}

		// PackageKit can list updates that cannot be committed together (for
		// example while an APT phased update is still deferred). Keep one
		// PackageKit session and job, but give every package its own transaction
		// so one package failure does not abandon the batch.
		trans, err := session.CreateTransaction(100)
		if err != nil {
			// A transaction cannot be created when the session itself is no
			// longer usable, so there is no meaningful next package to try.
			return err
		}

		reportPkgUpdateProgress(report, &PkgUpdateProgress{
			Type:       "status",
			PackageID:  packageID,
			Status:     fmt.Sprintf("Updating package %d of %d", index+1, len(packageIDs)),
			Percentage: new(uint32(uint64(index) * 100 / uint64(len(packageIDs)))),
		})

		err = updateOnePackageWithProgress(session.Context(), trans, packageID, index, len(packageIDs), report)
		pkgkit.LogClose(session.Context(), trans)
		if err == nil {
			updated++
			continue
		}
		if session.Context().Err() != nil {
			return session.Context().Err()
		}

		failures = append(failures, fmt.Sprintf("%s: %v", packageID, err))
		if firstFailure == nil {
			firstFailure = err
		}
		reportPkgUpdateProgress(report, &PkgUpdateProgress{
			Type:       "message",
			PackageID:  packageID,
			Status:     "Failed, continuing",
			Message:    fmt.Sprintf("Failed to update %s: %v. Continuing with remaining updates.", packageID, err),
			Percentage: new(uint32(uint64(index+1) * 100 / uint64(len(packageIDs)))),
		})
	}

	if len(failures) == 0 {
		return nil
	}
	if len(packageIDs) == 1 {
		return firstFailure
	}
	return fmt.Errorf(
		"updated %d of %d packages; %d failed: %s",
		updated,
		len(packageIDs),
		len(failures),
		strings.Join(failures, "; "),
	)
}

func updateOnePackageWithProgress(
	ctx context.Context,
	trans *pkgkit.Transaction,
	packageID string,
	index int,
	total int,
	report pkgUpdateReporter,
) error {
	// Flag 0 = no special flags (install normally).
	slog.Info("calling PackageKit UpdatePackages", "component", "dbus", "subsystem", "packagekit", "package_id", packageID)
	if err := trans.Call("UpdatePackages", uint64(0), []string{packageID}); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return awaitPackageUpdateSignals(waitCtx, trans, scalePackageProgress(report, index, total))
}

func scalePackageProgress(report pkgUpdateReporter, index int, total int) pkgUpdateReporter {
	return func(progress *PkgUpdateProgress) error {
		if progress == nil || total == 0 {
			return report(progress)
		}
		copy := *progress
		if progress.Percentage != nil {
			scaled := uint32((uint64(index)*100 + uint64(*progress.Percentage)) / uint64(total))
			copy.Percentage = &scaled
		}
		if progress.Type == "status" && progress.Status == "Finished" {
			copy.Status = fmt.Sprintf("Completed package %d of %d", index+1, total)
		}
		return report(&copy)
	}
}

func awaitPackageUpdateSignals(ctx context.Context, trans *pkgkit.Transaction, report pkgUpdateReporter) error {
	var lastWorkStatus uint32
	var transactionErr error
	for {
		select {
		case sig := <-trans.Signals():
			if sig == nil {
				continue
			}
			done, err := consumePackageUpdateSignal(
				report,
				sig,
				&lastWorkStatus,
				&transactionErr,
			)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func consumePackageUpdateSignal(
	report pkgUpdateReporter,
	sig *dbusclient.Signal,
	lastWorkStatus *uint32,
	transactionErr *error,
) (bool, error) {
	// PackageKit terminates failed transactions with ErrorCode followed by
	// Finished. Drain through Finished before starting the next package so
	// transactions never overlap while the backend unwinds the failed one.
	if sig.Name == pkgkit.TransactionIface+".ErrorCode" {
		if *transactionErr == nil {
			*transactionErr = packageUpdateSignalError(sig)
		}
		return false, nil
	}
	if sig.Name == pkgkit.TransactionIface+".Finished" && *transactionErr != nil {
		return true, *transactionErr
	}
	return handlePackageUpdateSignal(report, sig, pkgkit.TransactionIface, lastWorkStatus)
}

func handlePackageUpdateSignal(
	report pkgUpdateReporter,
	sig *dbusclient.Signal,
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

func handleItemProgressSignal(report pkgUpdateReporter, sig *dbusclient.Signal, lastWorkStatus *uint32) {
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

func handlePackageSignal(report pkgUpdateReporter, sig *dbusclient.Signal) {
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

func handleMessageSignal(report pkgUpdateReporter, sig *dbusclient.Signal) {
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

func packageUpdateSignalError(sig *dbusclient.Signal) error {
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
	sig *dbusclient.Signal,
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
	sig *dbusclient.Signal,
	transactionIfc string,
	lastWorkStatus uint32,
) (map[string]dbusclient.Variant, uint32, uint32, bool) {
	if len(sig.Body) < 2 {
		return nil, 0, 0, false
	}
	iface, _ := sig.Body[0].(string)
	if iface != transactionIfc {
		return nil, 0, 0, false
	}
	props, ok := sig.Body[1].(map[string]dbusclient.Variant)
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

func writePercentageProgress(report pkgUpdateReporter, props map[string]dbusclient.Variant, status uint32) {
	pct, ok := propertyUint32(props, "Percentage")
	// PackageKit reports 101 for "unknown"; forwarding it would make the bar
	// jump to 101% and then drop. Skip it so consumers keep the last value.
	if !ok || pct > 100 || !isRealWorkStatus(status) {
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

func propertyUint32(props map[string]dbusclient.Variant, key string) (uint32, bool) {
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
