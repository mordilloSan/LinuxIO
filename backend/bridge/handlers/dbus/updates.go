package dbus

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/internal/updates"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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

// —— use type ALIASES, not new structs —— //
type (
	AutoUpdateOptions = updates.AutoUpdateOptions
	AutoUpdateState   = updates.AutoUpdateState
)

func getAutoUpdates() (AutoUpdateState, error) {
	var out AutoUpdateState
	err := RetryOnceIfClosed(nil, func() error {
		systemDBusMu.Lock()
		defer systemDBusMu.Unlock()

		b := updates.SelectBackend()
		if b == nil {
			return fmt.Errorf("no supported backend found")
		}
		_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := b.Read()
		if err != nil {
			return err
		}
		out = st
		return nil
	})
	return out, err
}

func setAutoUpdates(jsonArg string) (AutoUpdateState, error) {
	var opts AutoUpdateOptions
	if err := json.Unmarshal([]byte(jsonArg), &opts); err != nil {
		return AutoUpdateState{}, fmt.Errorf("invalid JSON: %w", err)
	}

	var out AutoUpdateState
	err := RetryOnceIfClosed(nil, func() error {
		systemDBusMu.Lock()
		defer systemDBusMu.Unlock()

		b := updates.SelectBackend()
		if b == nil {
			return fmt.Errorf("no supported backend found")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := b.Apply(ctx, opts); err != nil { // opts now exactly matches backend type
			return err
		}
		st, err := b.Read()
		if err != nil {
			return err
		}
		out = st
		return nil
	})
	return out, err
}

func applyOfflineUpdates() (any, error) {
	return nil, RetryOnceIfClosed(nil, func() error {
		systemDBusMu.Lock()
		defer systemDBusMu.Unlock()

		b := updates.NewPkgKitBackendIfAvailable()
		if b == nil {
			return fmt.Errorf("PackageKit not available")
		}
		_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return b.ApplyOfflineNow()
	})
}

// --- Helpers ---

func extractCVEs(text string) []string {
	re := regexp.MustCompile(`CVE-\d{4}-\d+`)
	return re.FindAllString(text, -1)
}

// extractLatestChangelog extracts only the most recent changelog entry.
// Debian/Ubuntu changelogs have multiple version entries; we only want the first one.
func extractLatestChangelog(changelog string) string {
	if changelog == "" {
		return changelog
	}

	// Debian changelog format: each entry ends with a signature line like:
	// " -- Name <email>  Date"
	// followed by a blank line and then the next entry starts with:
	// "packagename (version) distribution; urgency=level"

	// Find where the second changelog entry starts (a line starting with a package name and version)
	// Pattern: two newlines followed by package name and (version)
	nextEntryPattern := regexp.MustCompile(`\n\n[a-zA-Z0-9][a-zA-Z0-9._+-]*\s+\([^)]+\)\s+[^;]+;`)
	matches := nextEntryPattern.FindStringIndex(changelog)

	if matches != nil {
		// Return everything before the next entry
		return strings.TrimSpace(changelog[:matches[0]])
	}

	return changelog
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

// GetUpdatesBasic returns package updates with basic info only (fast).
// This skips the slow GetUpdateDetail D-Bus call.
func GetUpdatesBasic() ([]UpdateDetail, error) {
	var result []UpdateDetail
	err := RetryOnceIfClosed(nil, func() error {
		updates, err := getUpdatesBasic()
		if err != nil {
			return err
		}
		if updates == nil {
			updates = make([]UpdateDetail, 0)
		}
		result = updates
		return nil
	})
	return result, err
}

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

// GetSingleUpdateDetail returns detailed info for a single package.
// Used for on-demand changelog fetching.
func GetSingleUpdateDetail(packageID string) (*UpdateDetail, error) {
	var result *UpdateDetail
	err := RetryOnceIfClosed(nil, func() error {
		detail, err := getSingleUpdateDetail(packageID)
		if err != nil {
			return err
		}
		result = detail
		return nil
	})
	return result, err
}

// --- Private Implementation ---

// getUpdatesBasic fetches available updates with basic info only (fast).
// Only calls GetUpdates, skips the slow GetUpdateDetail call.
func getUpdatesBasic() ([]UpdateDetail, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
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

	obj := conn.Object(pkBusName, godbus.ObjectPath(pkObjPath))
	var transPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction failed: %w", err)
	}
	trans := conn.Object(pkBusName, transPath)

	sigCh := make(chan *godbus.Signal, 20)
	conn.Signal(sigCh)
	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		logger.Errorf("Failed to add D-Bus match signal: %v", err)
	}

	getUpdatesCall := trans.Call(transactionIfc+".GetUpdates", 0, uint64(0))
	if getUpdatesCall.Err != nil {
		return nil, fmt.Errorf("GetUpdates failed: %w", getUpdatesCall.Err)
	}

	var updates []UpdateDetail
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

collectPackages:
	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				break collectPackages
			}
			if sig.Name == transactionIfc+".Package" {
				if len(sig.Body) > 2 {
					// Body[0] is the PkInfoEnum (uint32)
					infoEnum, _ := sig.Body[0].(uint32)
					pkgID, _ := sig.Body[1].(string)
					summary, _ := sig.Body[2].(string)
					name, version := extractNameVersion(pkgID)
					_ = name // unused, but extractNameVersion returns both

					// Sanitize invalid InfoEnum values (e.g., Docker repos have 327685 instead of valid 0-30 range)
					// PackageKit's valid severity range is 0-30. Values outside this are repository metadata bugs.
					if infoEnum > 30 {
						logger.Debugf(" Package %s has invalid InfoEnum: %d (sanitizing to 0=Unknown)", pkgID, infoEnum)
						infoEnum = 0 // PK_INFO_ENUM_UNKNOWN
					}

					updates = append(updates, UpdateDetail{
						PackageID: pkgID,
						Summary:   summary,
						Version:   version,
						// Other fields left empty - will be populated by GetUpdates (full) if needed
					})
				}
			} else if sig.Name == transactionIfc+".Finished" {
				break collectPackages
			}
		case <-ctx.Done():
			break collectPackages
		}
	}

	return updates, nil
}

// getSingleUpdateDetail fetches detailed info for a single package.
func getSingleUpdateDetail(packageID string) (*UpdateDetail, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
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

	obj := conn.Object(pkBusName, godbus.ObjectPath(pkObjPath))
	var transPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction failed: %w", err)
	}
	trans := conn.Object(pkBusName, transPath)

	sigCh := make(chan *godbus.Signal, 20)
	conn.Signal(sigCh)
	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		logger.Warnf("failed to add D-Bus match signal: %v", err)
	}

	// Call GetUpdateDetail for single package
	detailCall := trans.Call(transactionIfc+".GetUpdateDetail", 0, []string{packageID})
	if detailCall.Err != nil {
		return nil, fmt.Errorf("GetUpdateDetail failed: %w", detailCall.Err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var detail *UpdateDetail

collectDetail:
	for {
		select {
		case sig := <-sigCh:
			if sig == nil {
				break collectDetail
			}
			if sig.Name == transactionIfc+".UpdateDetail" {
				pkgID, _ := sig.Body[0].(string)
				if pkgID != packageID {
					continue
				}

				version, _ := sig.Body[11].(string)
				if version == "" {
					_, version = extractNameVersion(pkgID)
				}

				issued, _ := sig.Body[10].(string)
				changelogRaw, _ := sig.Body[8].(string)
				changelog := extractLatestChangelog(changelogRaw)
				cves := toStringSlice(sig.Body[5])
				restart, _ := sig.Body[6].(uint32)
				state, _ := sig.Body[9].(uint32)

				// Merge CVEs from changelog
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

				if issued == "" {
					issued = extractIssued(changelogRaw)
				}

				detail = &UpdateDetail{
					PackageID: pkgID,
					Version:   version,
					Issued:    issued,
					Changelog: changelog,
					CVEs:      combinedCVEs,
					Restart:   restart,
					State:     state,
				}
			} else if sig.Name == transactionIfc+".Finished" {
				break collectDetail
			}
		case <-ctx.Done():
			break collectDetail
		}
	}

	if detail == nil {
		return nil, fmt.Errorf("no details found for package %s", packageID)
	}
	return detail, nil
}

func getUpdatesWithDetails() ([]UpdateDetail, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	conn, err := godbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
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

	// 1. First transaction: GetUpdates
	obj := conn.Object(pkBusName, godbus.ObjectPath(pkObjPath))
	var updatesTransPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&updatesTransPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction failed: %w", err)
	}
	updatesTrans := conn.Object(pkBusName, updatesTransPath)

	updatesCh := make(chan *godbus.Signal, 20)
	conn.Signal(updatesCh)
	if err := conn.AddMatchSignal(
		godbus.WithMatchObjectPath(updatesTransPath),
	); err != nil {
		logger.Errorf("Failed to add D-Bus match signal: %v", err)
		// Optionally: return, or handle as needed
	}

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
	var detailsTransPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&detailsTransPath); err != nil {
		return nil, fmt.Errorf("CreateTransaction (for details) failed: %w", err)
	}
	detailsTrans := conn.Object(pkBusName, detailsTransPath)

	detailsCh := make(chan *godbus.Signal, 20)
	conn.Signal(detailsCh)
	if err := conn.AddMatchSignal(
		godbus.WithMatchObjectPath(detailsTransPath),
	); err != nil {
		logger.Warnf("failed to add D-Bus match signal for details transaction: %v", err)
	}

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
				pkgID, err := utils.AsString(sig.Body[0])
				if err != nil {
					return nil, fmt.Errorf("invalid pkgID: %w", err)
				}
				summary := summaryByPkg[pkgID]

				version, err := utils.AsString(sig.Body[11])
				if err != nil {
					return nil, fmt.Errorf("invalid version for %q: %w", pkgID, err)
				}
				if version == "" {
					_, version = extractNameVersion(pkgID)
				}

				issued, err := utils.AsString(sig.Body[10])
				if err != nil {
					return nil, fmt.Errorf("invalid issued date for %q: %w", pkgID, err)
				}

				changelogRaw, err := utils.AsString(sig.Body[8])
				if err != nil {
					return nil, fmt.Errorf("invalid changelog for %q: %w", pkgID, err)
				}
				changelog := extractLatestChangelog(changelogRaw)

				cves := toStringSlice(sig.Body[5])

				restart, err := utils.AsUint32(sig.Body[6])
				if err != nil {
					return nil, fmt.Errorf("invalid restart flag for %q: %w", pkgID, err)
				}

				state, err := utils.AsUint32(sig.Body[9])
				if err != nil {
					return nil, fmt.Errorf("invalid state for %q: %w", pkgID, err)
				}

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

// --- Update History (log parsing) ---

type UpgradeItem struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
}

type UpdateHistoryEntry struct {
	Date     string        `json:"date"`
	Upgrades []UpgradeItem `json:"upgrades"`
}

func GetUpdateHistory() ([]UpdateHistoryEntry, error) {
	if _, err := os.Stat("/var/log/dpkg.log"); err == nil {
		logger.Debugf("Parsing dpkg update history")
		return parseDpkgLog("/var/log/dpkg.log"), nil
	}
	if _, err := os.Stat("/var/log/dnf.log"); err == nil {
		logger.Debugf("Parsing dnf update history")
		return parseDnfHistory("/var/log/dnf.log"), nil
	}
	logger.Warnf("No known package manager log found")
	return []UpdateHistoryEntry{}, nil
}

func parseDpkgLog(logPath string) []UpdateHistoryEntry {
	file, err := os.Open(logPath)
	if err != nil {
		logger.Errorf("Failed to open dpkg log: %v", err)
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	installRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+(install|upgrade)\s+([^ ]+)\s+([^ ]+)`)
	configureRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+configure\s+([^ ]+)\s+([^ ]+)`)

	historyMap := make(map[string][]UpgradeItem)
	pendingPackages := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()

		if matches := installRe.FindStringSubmatch(line); len(matches) == 5 {
			date, pkg, version := matches[1], matches[3], matches[4]
			if version == "<none>" {
				pendingPackages[pkg] = date
			} else {
				historyMap[date] = append(historyMap[date], UpgradeItem{
					Package: pkg,
					Version: version,
				})
			}
		}

		if matches := configureRe.FindStringSubmatch(line); len(matches) == 4 {
			_, pkg, version := matches[1], matches[2], matches[3]
			if origDate, exists := pendingPackages[pkg]; exists {
				historyMap[origDate] = append(historyMap[origDate], UpgradeItem{
					Package: pkg,
					Version: version,
				})
				delete(pendingPackages, pkg)
			}
		}
	}

	return mapToSortedHistory(historyMap)
}

func parseDnfHistory(logPath string) []UpdateHistoryEntry {
	file, err := os.Open(logPath)
	if err != nil {
		logger.Errorf("Failed to open DNF log: %v", err)
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	upgradeRe := regexp.MustCompile(`Upgrade:\s+([^\s-]+)-([^-]+-[^\s]+)`)

	historyMap := make(map[string][]UpgradeItem)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 1 {
			continue
		}
		date := parts[0]

		if matches := upgradeRe.FindStringSubmatch(line); len(matches) > 2 {
			historyMap[date] = append(historyMap[date], UpgradeItem{
				Package: matches[1],
				Version: matches[2],
			})
		}
	}

	return mapToSortedHistory(historyMap)
}

func mapToSortedHistory(historyMap map[string][]UpgradeItem) []UpdateHistoryEntry {
	var dates []string
	for date := range historyMap {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	var history []UpdateHistoryEntry
	for _, date := range dates {
		history = append(history, UpdateHistoryEntry{
			Date:     date,
			Upgrades: historyMap[date],
		})
	}
	return history
}

func installPackage(packageID string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	return RetryOnceIfClosed(nil, func() error {
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

		// 1. Create Transaction
		obj := conn.Object(pkBusName, godbus.ObjectPath(pkObjPath))
		var transPath godbus.ObjectPath
		if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
			return fmt.Errorf("CreateTransaction failed: %w", err)
		}
		trans := conn.Object(pkBusName, transPath)

		// Listen for signals
		sigCh := make(chan *godbus.Signal, 20)
		conn.Signal(sigCh)
		if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
			logger.Warnf("failed to add D-Bus match signal: %v", err)
		}

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
	})
}
