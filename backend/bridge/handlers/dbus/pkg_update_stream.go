package dbus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// StreamTypePkgUpdate is the stream type for package update operations.
const StreamTypePkgUpdate = "pkg-update"

// RegisterStreamHandlers registers all dbus stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypePkgUpdate] = HandlePackageUpdateStream
}

// PkgUpdateProgress represents progress for package update operations.
type PkgUpdateProgress struct {
	Type       string `json:"type"`                  // "item_progress", "package", "status", "percentage"
	PackageID  string `json:"package_id,omitempty"`  // Current package being processed
	Status     string `json:"status,omitempty"`      // Status description (e.g., "Downloading", "Installing")
	StatusCode uint32 `json:"status_code,omitempty"` // PackageKit status enum
	Percentage uint32 `json:"percentage"`            // Overall or item percentage (0-100, 101=unknown)
	ItemPct    uint32 `json:"item_pct,omitempty"`    // Per-item percentage for ItemProgress
}

// writePkgUpdateProgress writes a package update progress frame to the stream.
func writePkgUpdateProgress(w io.Writer, streamID uint32, p *PkgUpdateProgress) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal pkg update progress: %w", err)
	}
	return ipc.WriteRelayFrame(w, &ipc.StreamFrame{
		Opcode:   ipc.OpStreamProgress,
		StreamID: streamID,
		Payload:  payload,
	})
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

// HandlePackageUpdateStream handles streaming package updates with real-time progress.
// args: package IDs to update (null-byte separated in payload)
func HandlePackageUpdateStream(sess *session.Session, stream net.Conn, args []string) error {
	logger.Debugf("[PkgUpdate] Starting with %d packages", len(args))

	if len(args) == 0 {
		_ = ipc.WriteResultError(stream, 0, "no packages specified", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("no packages specified")
	}

	// Send initial progress
	_ = writePkgUpdateProgress(stream, 0, &PkgUpdateProgress{
		Type:       "status",
		Status:     "Initializing",
		Percentage: 0,
	})

	err := updatePackagesWithProgress(stream, args)
	if err != nil {
		logger.Errorf("[PkgUpdate] Error: %v", err)
		_ = ipc.WriteResultError(stream, 0, err.Error(), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	_ = ipc.WriteResultOK(stream, 0, map[string]interface{}{
		"updated": len(args),
	})
	_ = ipc.WriteStreamClose(stream, 0)
	return nil
}

func updatePackagesWithProgress(stream net.Conn, packageIDs []string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	conn, err := godbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close D-Bus connection: %v", cerr)
		}
	}()

	const (
		pkBusName      = "org.freedesktop.PackageKit"
		pkObjPath      = "/org/freedesktop/PackageKit"
		transactionIfc = "org.freedesktop.PackageKit.Transaction"
	)

	// Create transaction
	obj := conn.Object(pkBusName, godbus.ObjectPath(pkObjPath))
	var transPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return fmt.Errorf("CreateTransaction failed: %w", err)
	}
	trans := conn.Object(pkBusName, transPath)

	// Listen for signals
	sigCh := make(chan *godbus.Signal, 100)
	conn.Signal(sigCh)
	defer conn.RemoveSignal(sigCh)

	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		logger.Warnf("failed to add D-Bus match signal: %v", err)
	}

	// Call UpdatePackages with all package IDs at once
	// Flag 0 = no special flags (install normally)
	logger.Debugf("[PkgUpdate] Calling UpdatePackages with %d packages", len(packageIDs))
	call := trans.Call(transactionIfc+".UpdatePackages", 0, uint64(0), packageIDs)
	if call.Err != nil {
		return fmt.Errorf("UpdatePackages failed: %w", call.Err)
	}

	// Process signals until Finished or error
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute) // Long timeout for large updates
	defer cancel()

	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				continue
			}

			switch sig.Name {
			case transactionIfc + ".ItemProgress":
				// ItemProgress(s id, u status, u percentage)
				if len(sig.Body) >= 3 {
					pkgID, _ := sig.Body[0].(string)
					status, _ := sig.Body[1].(uint32)
					pct, _ := sig.Body[2].(uint32)

					// Only show progress for real package work (not cache/prep)
					if !isRealWorkStatus(status) {
						continue
					}

					statusName := pkStatusNames[status]
					if statusName == "" {
						statusName = fmt.Sprintf("Status %d", status)
					}

					_ = writePkgUpdateProgress(stream, 0, &PkgUpdateProgress{
						Type:       "item_progress",
						PackageID:  pkgID,
						Status:     statusName,
						StatusCode: status,
						ItemPct:    pct,
					})
				}

			case transactionIfc + ".Package":
				// Package(u info, s package_id, s summary)
				if len(sig.Body) >= 2 {
					pkgID, _ := sig.Body[1].(string)
					_ = writePkgUpdateProgress(stream, 0, &PkgUpdateProgress{
						Type:      "package",
						PackageID: pkgID,
					})
				}

			case transactionIfc + ".Percentage":
				// Properties changed - check for Percentage
				// This comes as PropertiesChanged signal for Transaction
				// Skip - we handle percentage in ItemProgress

			case transactionIfc + ".ErrorCode":
				// ErrorCode(u code, s details)
				if len(sig.Body) >= 2 {
					code, _ := sig.Body[0].(uint32)
					details, _ := sig.Body[1].(string)
					return fmt.Errorf("PackageKit error %d: %s", code, details)
				}
				return fmt.Errorf("PackageKit error (unknown)")

			case transactionIfc + ".Finished":
				// Finished(u exit, u runtime)
				logger.Debugf("[PkgUpdate] Finished signal received")
				_ = writePkgUpdateProgress(stream, 0, &PkgUpdateProgress{
					Type:       "status",
					Status:     "Finished",
					Percentage: 100,
				})
				return nil

			case "org.freedesktop.DBus.Properties.PropertiesChanged":
				// Handle property changes for Percentage and Status
				if len(sig.Body) >= 2 {
					iface, _ := sig.Body[0].(string)
					if iface == transactionIfc {
						props, ok := sig.Body[1].(map[string]godbus.Variant)
						if ok {
							// Get status first to check if we should skip
							var currentStatus uint32
							if statusVar, exists := props["Status"]; exists {
								if s, ok := statusVar.Value().(uint32); ok {
									currentStatus = s
								}
							}

							// Only send percentage updates for real work statuses
							if pctVar, exists := props["Percentage"]; exists {
								if pct, ok := pctVar.Value().(uint32); ok && isRealWorkStatus(currentStatus) {
									_ = writePkgUpdateProgress(stream, 0, &PkgUpdateProgress{
										Type:       "percentage",
										Percentage: pct,
									})
								}
							}

							// Only send status updates for real work statuses
							if currentStatus > 0 && isRealWorkStatus(currentStatus) {
								statusName := pkStatusNames[currentStatus]
								if statusName == "" {
									statusName = fmt.Sprintf("Status %d", currentStatus)
								}
								_ = writePkgUpdateProgress(stream, 0, &PkgUpdateProgress{
									Type:       "status",
									Status:     statusName,
									StatusCode: currentStatus,
								})
							}
						}
					}
				}
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for package updates to complete")
		}
	}
}
