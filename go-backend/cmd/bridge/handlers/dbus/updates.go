package dbus

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

type UpdateDetail struct {
	PackageID string   `json:"package_id"`
	Summary   string   `json:"summary"`
	Version   string   `json:"version"`
	Issued    string   `json:"issued"`
	Changelog string   `json:"changelog"`
	CVEs      []string `json:"cve"`
	Restart   uint32   `json:"restart"`
	State     uint32   `json:"state"`
}

// --- Helpers ---

func extractCVEs(text string) []string {
	re := regexp.MustCompile(`CVE-\d{4}-\d+`)
	return re.FindAllString(text, -1)
}

func formatTextForHTML(text string) string {
	return strings.ReplaceAll(text, "\n", "<br>")
}

func extractIssued(changelog string) string {
	re := regexp.MustCompile(`(\w{3},\s*\d{1,2}\s*\w+\s*\d{4}\s*\d{2}:\d{2}:\d{2}\s*[-+]\d{4})`)
	match := re.FindStringSubmatch(changelog)
	if len(match) > 1 {
		t, err := time.Parse("Mon, 2 Jan 2006 15:04:05 -0700", match[1])
		if err == nil {
			return t.Format(time.RFC3339)
		}
		return match[1]
	}
	return ""
}

func extractNameVersion(packageID string) (name, version string) {
	parts := strings.Split(packageID, ";")
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return packageID, ""
}

func toStringSlice(iface any) []string {
	arr, ok := iface.([]interface{})
	if !ok {
		return []string{}
	}
	strs := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			strs = append(strs, s)
		}
	}
	return strs
}

// --- D-Bus Public Wrappers with Retry ---

func GetUpdatesWithDetails() ([]UpdateDetail, error) {
	var result []UpdateDetail
	err := RetryOnceIfClosed(nil, func() error {
		details, err := getUpdatesWithDetails()
		if err != nil {
			return err
		}
		if details == nil {
			details = make([]UpdateDetail, 0)
		}
		result = details
		return nil
	})
	return result, err
}

func InstallPackage(packageID string) error {
	return RetryOnceIfClosed(nil, func() error {
		return installPackage(packageID)
	})
}

// --- Private Implementation ---

func getUpdatesWithDetails() ([]UpdateDetail, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer conn.Close()

	const (
		pkBusName      = "org.freedesktop.PackageKit"
		pkObjPath      = "/org/freedesktop/PackageKit"
		transactionIfc = "org.freedesktop.PackageKit.Transaction"
	)

	// 1. First transaction: GetUpdates
	obj := conn.Object(pkBusName, dbus.ObjectPath(pkObjPath))
	var updatesTransPath dbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&updatesTransPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction failed: %w", err)
	}
	updatesTrans := conn.Object(pkBusName, updatesTransPath)

	updatesCh := make(chan *dbus.Signal, 20)
	conn.Signal(updatesCh)
	conn.AddMatchSignal(
		dbus.WithMatchObjectPath(updatesTransPath),
	)

	getUpdatesCall := updatesTrans.Call(transactionIfc+".GetUpdates", 0, uint64(0))
	if getUpdatesCall.Err != nil {
		return nil, fmt.Errorf("GetUpdates failed: %w", getUpdatesCall.Err)
	}

	var pkgIDs []string
	var summaries []string
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

collectPackages:
	for {
		select {
		case sig := <-updatesCh:
			if sig == nil {
				break collectPackages
			}
			if sig.Name == transactionIfc+".Package" {
				if len(sig.Body) > 2 {
					pkgID, _ := sig.Body[1].(string)
					summary, _ := sig.Body[2].(string)
					pkgIDs = append(pkgIDs, pkgID)
					summaries = append(summaries, summary)
				}
			} else if sig.Name == transactionIfc+".Finished" {
				break collectPackages
			}
		case <-ctx.Done():
			break collectPackages
		}
	}

	if len(pkgIDs) == 0 {
		return nil, nil
	}

	// 2. New transaction: GetUpdateDetail
	var detailsTransPath dbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&detailsTransPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction (for details) failed: %w", err)
	}
	detailsTrans := conn.Object(pkBusName, detailsTransPath)

	detailsCh := make(chan *dbus.Signal, 20)
	conn.Signal(detailsCh)
	conn.AddMatchSignal(
		dbus.WithMatchObjectPath(detailsTransPath),
	)

	detailCall := detailsTrans.Call(transactionIfc+".GetUpdateDetail", 0, pkgIDs)
	if detailCall.Err != nil {
		return nil, fmt.Errorf("GetUpdateDetail failed: %w", detailCall.Err)
	}

	var details []UpdateDetail
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	summaryByPkg := map[string]string{}
	for i, id := range pkgIDs {
		if i < len(summaries) {
			summaryByPkg[id] = summaries[i]
		}
	}

collectDetails:
	for {
		select {
		case sig := <-detailsCh:
			if sig == nil {
				break collectDetails
			}
			if sig.Name == transactionIfc+".UpdateDetail" {
				pkgID := sig.Body[0].(string)
				summary := summaryByPkg[pkgID]
				version := sig.Body[11].(string)
				if version == "" {
					_, version = extractNameVersion(pkgID)
				}

				issued := sig.Body[10].(string)
				changelogRaw := sig.Body[8].(string)
				changelog := formatTextForHTML(changelogRaw)
				cves := toStringSlice(sig.Body[5])
				restart := sig.Body[6].(uint32)
				state := sig.Body[9].(uint32)

				// Merge CVEs
				cveSet := make(map[string]struct{})
				for _, cve := range cves {
					cveSet[cve] = struct{}{}
				}
				for _, cve := range extractCVEs(changelogRaw) {
					cveSet[cve] = struct{}{}
				}
				combinedCVEs := make([]string, 0, len(cveSet))
				for cve := range cveSet {
					combinedCVEs = append(combinedCVEs, cve)
				}

				// Fix issued if needed
				if issued == "" {
					issued = extractIssued(changelogRaw)
				}

				detail := UpdateDetail{
					PackageID: pkgID,
					Summary:   summary,
					Version:   version,
					Issued:    issued,
					Changelog: changelog,
					CVEs:      combinedCVEs,
					Restart:   restart,
					State:     state,
				}
				details = append(details, detail)
			} else if sig.Name == transactionIfc+".Finished" {
				break collectDetails
			}
		case <-ctx2.Done():
			break collectDetails
		}
	}

	return details, nil
}

func installPackage(packageID string) error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer conn.Close()

	const (
		pkBusName      = "org.freedesktop.PackageKit"
		pkObjPath      = "/org/freedesktop/PackageKit"
		transactionIfc = "org.freedesktop.PackageKit.Transaction"
	)

	// 1. Create Transaction
	obj := conn.Object(pkBusName, dbus.ObjectPath(pkObjPath))
	var transPath dbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return fmt.Errorf("CreateTransaction failed: %w", err)
	}
	trans := conn.Object(pkBusName, transPath)

	// Listen for signals
	sigCh := make(chan *dbus.Signal, 20)
	conn.Signal(sigCh)
	conn.AddMatchSignal(dbus.WithMatchObjectPath(transPath))

	// 2. Call InstallPackages
	call := trans.Call(transactionIfc+".InstallPackages", 0, uint64(0), []string{packageID})
	if call.Err != nil {
		return fmt.Errorf("InstallPackages failed: %w", call.Err)
	}

	// 3. Wait for Finished/ErrorCode signal
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				return fmt.Errorf("nil signal from D-Bus")
			}
			switch sig.Name {
			case transactionIfc + ".ErrorCode":
				code, _ := sig.Body[0].(uint32)
				msg, _ := sig.Body[1].(string)
				return fmt.Errorf("PackageKit error code %d: %s", code, msg)
			case transactionIfc + ".Finished":
				// Success!
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for PackageKit to finish install")
		}
	}
}
