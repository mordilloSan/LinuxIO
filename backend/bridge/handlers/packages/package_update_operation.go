package packages

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

func packageUpdateBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.MutationTask("packages.update", runPackageUpdateTask, apischema.Privileged(), apischema.Progress[PkgUpdateProgress](), apischema.WithTaskMetadata(func(req apischema.PackageUpdateRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: append([]string{}, req.PackageIDs...), Label: "Updating packages", PackageIDs: append([]string{}, req.PackageIDs...)}
		}), apischema.Exclusive()).Resumable(apischema.ResumeHooks[apischema.PackageUpdateRequest, PackageUpdateResult]{
			Suspend: suspendPackageUpdate,
			Resume:  resumePackageUpdate,
			// PackageKit runs a transaction on when the job host dies, not
			// across a reboot, after which the job stays interrupted. A stop
			// by hand may be meant to halt the batch, so it is not picked up.
			At: bridgeipc.ResumeAfterCrash,
		}),
	)
}

// PkgUpdateProgress represents progress for package update operations.
type PkgUpdateProgress struct {
	Type           string  `json:"type"`                      // "item_progress", "package", "status", "percentage", "message", "output" (a dpkg terminal line in Message)
	PackageID      string  `json:"package_id,omitempty"`      // Current package being processed
	PackageSummary string  `json:"package_summary,omitempty"` // Package summary from Package(...) signal
	Status         string  `json:"status,omitempty"`          // Status description (e.g., "Downloading", "Installing")
	Message        string  `json:"message,omitempty"`         // Rich backend message text when available
	StatusCode     *uint32 `json:"status_code,omitzero"`      // PackageKit status enum
	InfoCode       *uint32 `json:"info_code,omitzero"`        // PackageKit info enum (Package signal)
	Percentage     *uint32 `json:"percentage,omitzero"`       // Overall percentage (0-100, 101=unknown)
	ItemPct        *uint32 `json:"item_pct,omitzero"`         // Per-item percentage for ItemProgress
}

func (p PkgUpdateProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	var percentage *int
	progressPercentage := p.Percentage
	if progressPercentage == nil {
		progressPercentage = p.ItemPct
	}
	if progressPercentage != nil && *progressPercentage <= 100 {
		value := int(*progressPercentage)
		percentage = &value
	}
	message := p.Message
	if message == "" {
		message = p.Status
	}
	return bridgeipc.TaskProgress{
		Percentage: percentage,
		Phase:      p.Type,
		Message:    message,
		Detail:     p,
	}
}

type PackageUpdateResult struct {
	Updated int `json:"updated"`
}

type pkgUpdateReporter func(*PkgUpdateProgress) error

func taskPkgUpdateReporter(task *bridgeipc.Task) pkgUpdateReporter {
	return func(p *PkgUpdateProgress) error {
		if p.Type == "output" {
			// Like capability installer output: direct watchers only, never
			// durable task state or a tasks.events notification.
			task.ReportTransientProgress(p.ProgressEnvelope())
			return nil
		}
		task.ReportProgress(*p)
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

func runPackageUpdateTask(ctx context.Context, task *bridgeipc.Task, req apischema.PackageUpdateRequest) (PackageUpdateResult, error) {
	if len(req.PackageIDs) == 0 {
		return PackageUpdateResult{}, fmt.Errorf("%w: packages", bridgeipc.ErrInvalidArgs)
	}
	report := taskPkgUpdateReporter(task)
	reportPkgUpdateProgress(report, &PkgUpdateProgress{
		Type:       "status",
		Status:     "Initializing",
		Percentage: new(uint32(0)),
	})

	// A snapshot failure is reported but never blocks the update.
	snapshotReport, snapshotErr := preUpdateSnapshot(ctx)
	if len(snapshotReport.Taken) > 0 {
		reportPkgUpdateProgress(report, &PkgUpdateProgress{Type: "message", Status: "Took btrfs pre-update snapshot"})
	}
	if msg := preUpdateSnapshotProblems(snapshotReport, snapshotErr); msg != "" {
		slog.Warn("btrfs pre-update snapshot problems; applying package updates anyway", "problems", msg)
		reportPkgUpdateProgress(report, &PkgUpdateProgress{Type: "message", Status: "btrfs pre-update snapshot failed: " + msg})
	}

	if err := updatePackages(ctx, req.PackageIDs, report); err != nil {
		if ctx.Err() != nil {
			return PackageUpdateResult{}, context.Canceled
		}
		return PackageUpdateResult{}, err
	}

	result := PackageUpdateResult{Updated: len(req.PackageIDs)}
	return result, nil
}

// packageCheckpoint is where a package update job stands: the package it is
// at, the PackageKit transaction updating it, and how the packages before it
// went. The job saves it in its record before it asks PackageKit to update
// each package, so a job host that starts after this one died picks the job up
// from PackageKit instead of updating that package again.
type packageCheckpoint struct {
	Index       int      `json:"index"`
	Transaction string   `json:"transaction"`
	Updated     int      `json:"updated"`
	Failures    []string `json:"failures,omitzero"`
}

// saveCheckpoint is a test seam.
var saveCheckpoint = bridgeipc.SaveCheckpoint

// packageBatch updates a job's packages, each in a transaction of its own,
// and counts how they went.
type packageBatch struct {
	ids      []string
	report   pkgUpdateReporter
	updated  int
	failures []string
	// first is the first package's failure, which a batch of one returns as it is.
	first error
}

func updatePackages(ctx context.Context, packageIDs []string, report pkgUpdateReporter) error {
	batch := &packageBatch{ids: packageIDs, report: report}
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		return batch.run(session, 0)
	})
}

// run updates the packages from index from on, then returns how the batch went.
func (b *packageBatch) run(session pkgkit.ClientSession, from int) error {
	ctx := session.Context()
	for index := from; index < len(b.ids); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		packageID := b.ids[index]

		// PackageKit can list updates that cannot be committed together (for
		// example while an APT phased update is still deferred). Keep one
		// PackageKit session and task, but give every package its own transaction
		// so one package failure does not abandon the batch.
		trans, err := session.CreateTransaction(100)
		if err != nil {
			// A transaction cannot be created when the session itself is no
			// longer usable, so there is no meaningful next package to try.
			return err
		}
		// Saved before the update starts: a pick-up must find the transaction
		// that may be updating this package, never an earlier one.
		if err = saveCheckpoint(ctx, b.checkpoint(index, trans.Path())); err != nil {
			pkgkit.LogClose(ctx, trans)
			return fmt.Errorf("save where the package update stands: %w", err)
		}

		reportPkgUpdateProgress(b.report, &PkgUpdateProgress{
			Type:       "status",
			PackageID:  packageID,
			Status:     fmt.Sprintf("Updating package %d of %d", index+1, len(b.ids)),
			Percentage: new(uint32(uint64(index) * 100 / uint64(len(b.ids)))),
		})

		err = updateOnePackage(ctx, trans, packageID, index, len(b.ids), b.report)
		pkgkit.LogClose(ctx, trans)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		b.done(index, err)
	}
	return b.outcome()
}

func (b *packageBatch) checkpoint(index int, transaction dbusclient.ObjectPath) packageCheckpoint {
	return packageCheckpoint{Index: index, Transaction: string(transaction), Updated: b.updated, Failures: b.failures}
}

// done counts how the update of package index went: err is its failure.
func (b *packageBatch) done(index int, err error) {
	packageID := b.ids[index]
	if err == nil {
		b.updated++
		slog.Debug("package updated", "package_id", packageID)
		return
	}
	b.failures = append(b.failures, fmt.Sprintf("%s: %v", packageID, err))
	if b.first == nil {
		b.first = err
	}
	reportPkgUpdateProgress(b.report, &PkgUpdateProgress{
		Type:       "message",
		PackageID:  packageID,
		Status:     "Failed, continuing",
		Message:    fmt.Sprintf("Failed to update %s: %v. Continuing with remaining updates.", packageID, err),
		Percentage: new(uint32(uint64(index+1) * 100 / uint64(len(b.ids)))),
	})
}

// outcome is how the whole batch went.
func (b *packageBatch) outcome() error {
	if b.updated > 0 {
		slog.Info("packages updated", "count", b.updated, "failed", len(b.failures))
	}
	if len(b.failures) == 0 {
		return nil
	}
	if len(b.ids) == 1 && b.first != nil {
		return b.first
	}
	return fmt.Errorf(
		"updated %d of %d packages; %d failed: %s",
		b.updated,
		len(b.ids),
		len(b.failures),
		strings.Join(b.failures, "; "),
	)
}

func updateOnePackage(
	ctx context.Context,
	trans *pkgkit.Transaction,
	packageID string,
	index int,
	total int,
	report pkgUpdateReporter,
) error {
	defer cancelIfCanceled(ctx, trans)
	// Flag 0 = no special flags (install normally).
	slog.Debug("calling PackageKit UpdatePackages", "component", "dbus", "subsystem", "packagekit", "package_id", packageID)
	termLog := openAptTermLog()
	defer termLog.Close()
	if err := trans.Call("UpdatePackages", uint64(0), []string{packageID}); err != nil {
		return err
	}
	return awaitTransaction(ctx, trans, termLog, scalePackageProgress(report, index, total))
}

// awaitTransaction relays a package's transaction until it ends and returns
// its failure.
func awaitTransaction(ctx context.Context, trans *pkgkit.Transaction, termLog *aptTermLog, report pkgUpdateReporter) error {
	waitCtx, cancel := context.WithTimeout(ctx, packageKitTransactionTimeout)
	defer cancel()
	return awaitPackageUpdateSignals(waitCtx, trans.Signals(), termLog, report)
}

// cancelIfCanceled asks PackageKit to cancel the job's transaction trans when
// the job's owner canceled the job. When its host stops the job instead, the
// transaction runs on, for the next host to pick the job up.
func cancelIfCanceled(ctx context.Context, trans *pkgkit.Transaction) {
	if ctx.Err() == nil || hostStopping(ctx) {
		return
	}
	cancelCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), transactionCancelTimeout)
	defer stop()
	err := trans.Cancel(cancelCtx)
	switch {
	case errors.Is(err, pkgkit.ErrCannotCancel):
		slog.Debug("package update transaction not canceled", "transaction", trans.Path(), "error", err)
	case err != nil:
		slog.Warn("package update transaction not canceled", "transaction", trans.Path(), "error", err)
	}
}

// transactionCancelTimeout bounds PackageKit's answer to a Cancel.
const transactionCancelTimeout = 10 * time.Second

// hostStopping reports whether the job host is stopping the job in ctx,
// rather than its owner canceling it.
func hostStopping(ctx context.Context) bool {
	task, ok := bridgeipc.TaskFromContext(ctx)
	return ok && task.Interrupted()
}

// suspendPackageUpdate keeps the checkpoint the job saved as it went: the
// context cancel that follows stops following the transaction, which
// PackageKit runs on.
func suspendPackageUpdate(context.Context, apischema.PackageUpdateRequest, jsontext.Value) (jsontext.Value, error) {
	return nil, nil
}

// resumePackageUpdate picks a package update up again at the root host's next
// start after its host crashed in this boot.
func resumePackageUpdate(ctx context.Context, req apischema.PackageUpdateRequest, checkpoint jsontext.Value) (PackageUpdateResult, error) {
	task, _ := bridgeipc.TaskFromContext(ctx) // Resume always runs as a task
	if err := pickUpPackageUpdate(ctx, req.PackageIDs, checkpoint, taskPkgUpdateReporter(task)); err != nil {
		if ctx.Err() != nil {
			return PackageUpdateResult{}, context.Canceled
		}
		return PackageUpdateResult{}, err
	}
	return PackageUpdateResult{Updated: len(req.PackageIDs)}, nil
}

// historyLookback is how many of PackageKit's newest history entries a
// pick-up reads to find its transaction.
const historyLookback = 200

// pickUpPackageUpdate continues the batch of packageIDs from checkpoint.
// PackageKit runs a transaction to its end whoever follows it, so the job
// follows the transaction it was in, or reads how it ended from PackageKit's
// history, and then updates the packages it had not reached. It never asks
// PackageKit to update a package again: when PackageKit cannot say what
// became of the transaction, it returns bridgeipc.ErrNotResumed and the job
// stays interrupted.
func pickUpPackageUpdate(ctx context.Context, packageIDs []string, checkpoint jsontext.Value, report pkgUpdateReporter) error {
	cp, err := decodePackageCheckpoint(checkpoint, len(packageIDs))
	if err != nil {
		return err
	}
	batch := &packageBatch{ids: packageIDs, report: report, updated: cp.Updated, failures: cp.Failures}
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		reportPkgUpdateProgress(report, &PkgUpdateProgress{
			Type:       "status",
			PackageID:  packageIDs[cp.Index],
			Status:     fmt.Sprintf("Updating package %d of %d", cp.Index+1, len(packageIDs)),
			Percentage: new(uint32(uint64(cp.Index) * 100 / uint64(len(packageIDs)))),
		})
		failure, err := transactionOutcome(session, cp, len(packageIDs), report)
		if errors.Is(err, bridgeipc.ErrNotResumed) {
			return fmt.Errorf("%w; it was updating %s, %d of the %d packages before it were updated and %d were not reached",
				err, packageIDs[cp.Index], cp.Updated, cp.Index, len(packageIDs)-cp.Index-1)
		}
		if err != nil {
			return err
		}
		slog.Debug("picked up package update", "transaction", cp.Transaction, "package_id", packageIDs[cp.Index], "updated", failure == nil)
		batch.done(cp.Index, failure)
		return batch.run(session, cp.Index+1)
	})
}

func decodePackageCheckpoint(raw jsontext.Value, packages int) (packageCheckpoint, error) {
	if len(raw) == 0 {
		return packageCheckpoint{}, fmt.Errorf("%w: its job host stopped before PackageKit was asked to update a package", bridgeipc.ErrNotResumed)
	}
	var cp packageCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return packageCheckpoint{}, fmt.Errorf("%w: its checkpoint is unreadable: %v", bridgeipc.ErrNotResumed, err)
	}
	if cp.Index < 0 || cp.Index >= packages || !dbusclient.ObjectPath(cp.Transaction).IsValid() {
		return packageCheckpoint{}, fmt.Errorf("%w: its checkpoint names no transaction of its packages", bridgeipc.ErrNotResumed)
	}
	return cp, nil
}

// transactionOutcome is how the transaction of cp ended for its package:
// failure is nil for a package updated. It follows the transaction while
// PackageKit runs it, and otherwise reads PackageKit's history. err is
// bridgeipc.ErrNotResumed when PackageKit cannot account for it.
func transactionOutcome(session pkgkit.ClientSession, cp packageCheckpoint, total int, report pkgUpdateReporter) (failure, err error) {
	ctx := session.Context()
	path := dbusclient.ObjectPath(cp.Transaction)
	// Watched before asking, so an end between the two is not missed.
	trans, err := session.WatchTransaction(path, 100)
	if err != nil {
		return nil, err
	}
	defer pkgkit.LogClose(ctx, trans)
	running, err := session.TransactionRunning(path)
	if err != nil {
		return nil, err
	}
	if running {
		role, roleErr := trans.Role()
		switch {
		case roleErr != nil:
			// It ended and PackageKit dropped it between the two calls: its
			// history tells.
			slog.Debug("PackageKit transaction gone before its role was read", "transaction", path, "error", roleErr)
		case role != pkgkit.RoleUpdatePackages:
			return nil, fmt.Errorf("%w: PackageKit transaction %s is not a package update", bridgeipc.ErrNotResumed, path)
		default:
			defer cancelIfCanceled(ctx, trans)
			termLog := openAptTermLog()
			defer termLog.Close()
			failure := awaitTransaction(ctx, trans, termLog, scalePackageProgress(report, cp.Index, total))
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return failure, nil
		}
	}
	old, found, err := session.FindOldTransaction(path, historyLookback)
	switch {
	case err != nil:
		return nil, err
	case !found || old.Role != pkgkit.RoleUpdatePackages:
		// The job creates the transaction, saves it, then asks for the
		// update: a host that died in between leaves one PackageKit never ran.
		return nil, fmt.Errorf("%w: PackageKit neither runs nor recorded its transaction %s, which its job host may have stopped before using", bridgeipc.ErrNotResumed, path)
	case old.Succeeded:
		return nil, nil
	case old.Duration == 0:
		return nil, fmt.Errorf("%w: PackageKit recorded no end of transaction %s", bridgeipc.ErrNotResumed, path)
	default:
		// PackageKit's history keeps no error text.
		return fmt.Errorf("PackageKit recorded transaction %s as failed", path), nil
	}
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

// awaitPackageUpdateSignals relays one transaction's signals, and termLog's
// dpkg lines (nil termLog: none), on this goroutine so reporters need no locks.
func awaitPackageUpdateSignals(ctx context.Context, signals <-chan *dbusclient.Signal, termLog *aptTermLog, report pkgUpdateReporter) error {
	var lastWorkStatus uint32
	var transactionErr error
	var poll <-chan time.Time
	if termLog != nil {
		ticker := time.NewTicker(aptTermLogPollInterval)
		defer ticker.Stop()
		poll = ticker.C
	}
	for {
		select {
		case sig, ok := <-signals:
			if !ok {
				return pkgkit.ErrSignalsClosed
			}
			done, err := consumePackageUpdateSignal(
				report,
				sig,
				&lastWorkStatus,
				&transactionErr,
			)
			if done || err != nil {
				reportAptTermLog(report, termLog)
			}
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case <-poll:
			reportAptTermLog(report, termLog)
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
	// Any exit but success is a package not updated: a follow that began
	// between ErrorCode and Finished, as a pick-up can, sees only the exit,
	// and PackageKit ends some transactions (key or EULA required, skipped)
	// with no ErrorCode.
	if sig.Name == pkgkit.TransactionIface+".Finished" && len(sig.Body) > 0 {
		if exit, _ := sig.Body[0].(uint32); exit > pkgkit.ExitSuccess {
			return true, fmt.Errorf("PackageKit transaction ended with exit %d", exit)
		}
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
	slog.Debug("finished signal received")
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
