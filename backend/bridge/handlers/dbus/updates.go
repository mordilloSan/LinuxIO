package dbus

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	InfoEnum  uint32   `json:"info_enum,omitempty"` // PackageKit info enum (severity/type): 0=Unknown, 1-30=various types
}

// —— use type ALIASES, not new structs —— //
type (
	AutoUpdateOptions = updates.AutoUpdateOptions
	AutoUpdateState   = updates.AutoUpdateState
)

const (
	packageKitBusName        = "org.freedesktop.PackageKit"
	packageKitObjPath        = "/org/freedesktop/PackageKit"
	packageKitTransactionIfc = "org.freedesktop.PackageKit.Transaction"
)

type packageUpdateMeta struct {
	Summary  string
	InfoEnum uint32
}

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
	arr, ok := iface.([]any)
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

func sanitizeInfoEnum(pkgID string, infoEnum uint32) uint32 {
	if infoEnum <= 30 {
		return infoEnum
	}

	logger.Debugf(" Package %s has invalid InfoEnum: %d (sanitizing to 0=Unknown)", pkgID, infoEnum)
	return 0
}

func mergeUpdateCVEs(changelogRaw string, cves []string) []string {
	cveSet := make(map[string]struct{}, len(cves))
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
	return combinedCVEs
}

func newPackageKitTransaction(conn *godbus.Conn) (godbus.BusObject, godbus.ObjectPath, error) {
	obj := conn.Object(packageKitBusName, godbus.ObjectPath(packageKitObjPath))

	var transPath godbus.ObjectPath
	if err := obj.Call("org.freedesktop.PackageKit.CreateTransaction", 0).Store(&transPath); err != nil {
		return nil, "", fmt.Errorf("CreateTransaction failed: %w", err)
	}

	return conn.Object(packageKitBusName, transPath), transPath, nil
}

func watchTransactionSignals(conn *godbus.Conn, transPath godbus.ObjectPath, addMatchMessage string) (chan *godbus.Signal, func()) {
	sigCh := make(chan *godbus.Signal, 20)
	conn.Signal(sigCh)

	if err := conn.AddMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
		logger.Warnf(addMatchMessage, err)
	}

	cleanup := func() {
		conn.RemoveSignal(sigCh)
		if err := conn.RemoveMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
			logger.Debugf("failed to remove D-Bus match signal: %v", err)
		}
	}

	return sigCh, cleanup
}

func callPackageKitTransaction(trans godbus.BusObject, method string, args ...any) error {
	call := trans.Call(packageKitTransactionIfc+"."+method, 0, args...)
	if call.Err != nil {
		return fmt.Errorf("%s failed: %w", method, call.Err)
	}
	return nil
}

func isTransactionFinished(sig *godbus.Signal) bool {
	return sig == nil || sig.Name == packageKitTransactionIfc+".Finished"
}

func readPackageSignal(sig *godbus.Signal) (string, packageUpdateMeta, bool) {
	if sig.Name != packageKitTransactionIfc+".Package" || len(sig.Body) <= 2 {
		return "", packageUpdateMeta{}, false
	}

	infoEnum, _ := sig.Body[0].(uint32)
	pkgID, _ := sig.Body[1].(string)
	summary, _ := sig.Body[2].(string)

	return pkgID, packageUpdateMeta{
		Summary:  summary,
		InfoEnum: sanitizeInfoEnum(pkgID, infoEnum),
	}, true
}

func collectUpdatePackages(ctx context.Context, sigCh <-chan *godbus.Signal) ([]string, map[string]packageUpdateMeta) {
	var pkgIDs []string
	metaByPkg := make(map[string]packageUpdateMeta)

	for {
		select {
		case sig := <-sigCh:
			if isTransactionFinished(sig) {
				return pkgIDs, metaByPkg
			}

			pkgID, meta, ok := readPackageSignal(sig)
			if !ok {
				continue
			}

			pkgIDs = append(pkgIDs, pkgID)
			metaByPkg[pkgID] = meta
		case <-ctx.Done():
			return pkgIDs, metaByPkg
		}
	}
}

func buildBasicUpdates(pkgIDs []string, metaByPkg map[string]packageUpdateMeta) []UpdateDetail {
	updates := make([]UpdateDetail, 0, len(pkgIDs))
	for _, pkgID := range pkgIDs {
		_, version := extractNameVersion(pkgID)
		meta := metaByPkg[pkgID]
		updates = append(updates, UpdateDetail{
			PackageID: pkgID,
			Summary:   meta.Summary,
			Version:   version,
			InfoEnum:  meta.InfoEnum,
		})
	}
	return updates
}

func buildUpdateDetail(body []any, summary string, infoEnum uint32) (UpdateDetail, error) {
	pkgID, err := utils.AsString(body[0])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid pkgID: %w", err)
	}

	version, err := utils.AsString(body[11])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid version for %q: %w", pkgID, err)
	}
	if version == "" {
		_, version = extractNameVersion(pkgID)
	}

	issued, err := utils.AsString(body[10])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid issued date for %q: %w", pkgID, err)
	}

	changelogRaw, err := utils.AsString(body[8])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid changelog for %q: %w", pkgID, err)
	}

	restart, err := utils.AsUint32(body[6])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid restart flag for %q: %w", pkgID, err)
	}

	state, err := utils.AsUint32(body[9])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid state for %q: %w", pkgID, err)
	}

	if issued == "" {
		issued = extractIssued(changelogRaw)
	}

	return UpdateDetail{
		PackageID: pkgID,
		Summary:   summary,
		Version:   version,
		Issued:    issued,
		Changelog: extractLatestChangelog(changelogRaw),
		CVEs:      mergeUpdateCVEs(changelogRaw, toStringSlice(body[5])),
		Restart:   restart,
		State:     state,
		InfoEnum:  infoEnum,
	}, nil
}

func collectSingleUpdateDetail(ctx context.Context, sigCh <-chan *godbus.Signal, packageID string) (*UpdateDetail, error) {
	var detail *UpdateDetail

	for {
		select {
		case sig := <-sigCh:
			if isTransactionFinished(sig) {
				return finalizeSingleUpdateDetail(detail, packageID)
			}
			if sig.Name != packageKitTransactionIfc+".UpdateDetail" {
				continue
			}

			current, err := buildUpdateDetail(sig.Body, "", 0)
			if err != nil {
				return nil, err
			}
			if current.PackageID == packageID {
				detail = &current
			}
		case <-ctx.Done():
			return finalizeSingleUpdateDetail(detail, packageID)
		}
	}
}

func finalizeSingleUpdateDetail(detail *UpdateDetail, packageID string) (*UpdateDetail, error) {
	if detail == nil {
		return nil, fmt.Errorf("no details found for package %s", packageID)
	}
	return detail, nil
}

func collectUpdateDetails(ctx context.Context, sigCh <-chan *godbus.Signal, metaByPkg map[string]packageUpdateMeta) ([]UpdateDetail, error) {
	var details []UpdateDetail

	for {
		select {
		case sig := <-sigCh:
			if isTransactionFinished(sig) {
				return details, nil
			}
			if sig.Name != packageKitTransactionIfc+".UpdateDetail" {
				continue
			}

			pkgID, err := utils.AsString(sig.Body[0])
			if err != nil {
				return nil, fmt.Errorf("invalid pkgID: %w", err)
			}

			meta := metaByPkg[pkgID]
			detail, err := buildUpdateDetail(sig.Body, meta.Summary, meta.InfoEnum)
			if err != nil {
				return nil, err
			}

			details = append(details, detail)
		case <-ctx.Done():
			return details, nil
		}
	}
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

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close D-Bus connection: %v", cerr)
		}
	}()

	trans, transPath, err := newPackageKitTransaction(conn)
	if err != nil {
		return nil, err
	}
	sigCh, cleanup := watchTransactionSignals(conn, transPath, "Failed to add D-Bus match signal: %v")
	defer cleanup()

	if err := callPackageKitTransaction(trans, "GetUpdates", uint64(0)); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pkgIDs, metaByPkg := collectUpdatePackages(ctx, sigCh)
	return buildBasicUpdates(pkgIDs, metaByPkg), nil
}

// getSingleUpdateDetail fetches detailed info for a single package.
func getSingleUpdateDetail(packageID string) (*UpdateDetail, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close D-Bus connection: %v", cerr)
		}
	}()

	trans, transPath, err := newPackageKitTransaction(conn)
	if err != nil {
		return nil, err
	}
	sigCh, cleanup := watchTransactionSignals(conn, transPath, "failed to add D-Bus match signal: %v")
	defer cleanup()

	if err := callPackageKitTransaction(trans, "GetUpdateDetail", []string{packageID}); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return collectSingleUpdateDetail(ctx, sigCh, packageID)
}

func getUpdatesWithDetails() ([]UpdateDetail, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			logger.Warnf("failed to close D-Bus connection: %v", cerr)
		}
	}()

	// 1. First transaction: GetUpdates
	updatesTrans, updatesTransPath, err := newPackageKitTransaction(conn)
	if err != nil {
		return nil, err
	}
	updatesCh, cleanupUpdates := watchTransactionSignals(conn, updatesTransPath, "Failed to add D-Bus match signal: %v")
	defer cleanupUpdates()

	if callErr := callPackageKitTransaction(updatesTrans, "GetUpdates", uint64(0)); callErr != nil {
		return nil, callErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pkgIDs, metaByPkg := collectUpdatePackages(ctx, updatesCh)

	if len(pkgIDs) == 0 {
		return nil, nil
	}

	// 2. New transaction: GetUpdateDetail
	detailsTrans, detailsTransPath, err := newPackageKitTransaction(conn)
	if err != nil {
		return nil, err
	}
	detailsCh, cleanupDetails := watchTransactionSignals(conn, detailsTransPath, "failed to add D-Bus match signal for details transaction: %v")
	defer cleanupDetails()

	if callErr := callPackageKitTransaction(detailsTrans, "GetUpdateDetail", pkgIDs); callErr != nil {
		return nil, callErr
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	return collectUpdateDetails(ctx2, detailsCh, metaByPkg)
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
		return parseDpkgLogs(), nil
	}
	if _, err := os.Stat("/var/log/dnf.log"); err == nil {
		logger.Debugf("Parsing dnf update history")
		return parseDnfHistory("/var/log/dnf.log"), nil
	}
	logger.Warnf("No known package manager log found")
	return []UpdateHistoryEntry{}, nil
}

// parseDpkgLogs reads dpkg.log plus all rotated variants (.1, .2.gz, …).
func parseDpkgLogs() []UpdateHistoryEntry {
	historyMap := make(map[string][]UpgradeItem)
	pendingPackages := make(map[string]string)

	// Collect all dpkg log files (dpkg.log, dpkg.log.1, dpkg.log.2.gz, …)
	matches, _ := filepath.Glob("/var/log/dpkg.log*")
	// Sort in reverse so oldest logs are parsed first (pending state resolves correctly)
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	installRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+(install|upgrade)\s+([^ ]+)\s+([^ ]+)`)
	configureRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+configure\s+([^ ]+)\s+([^ ]+)`)

	for _, logPath := range matches {
		reader, closer, err := openLogFile(logPath)
		if err != nil {
			logger.Warnf("Failed to open %s: %v", logPath, err)
			continue
		}

		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()

			if m := installRe.FindStringSubmatch(line); len(m) == 5 {
				date, pkg, version := m[1], m[3], m[4]
				if version == "<none>" {
					pendingPackages[pkg] = date
				} else {
					historyMap[date] = append(historyMap[date], UpgradeItem{
						Package: pkg,
						Version: version,
					})
				}
			}

			if m := configureRe.FindStringSubmatch(line); len(m) == 4 {
				_, pkg, version := m[1], m[2], m[3]
				if origDate, exists := pendingPackages[pkg]; exists {
					historyMap[origDate] = append(historyMap[origDate], UpgradeItem{
						Package: pkg,
						Version: version,
					})
					delete(pendingPackages, pkg)
				}
			}
		}

		closer()
	}

	return mapToSortedHistory(historyMap)
}

// openLogFile opens a plain or gzipped log file and returns an io.Reader and a close function.
func openLogFile(path string) (io.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		return gz, func() { gz.Close(); f.Close() }, nil
	}

	return f, func() { f.Close() }, nil
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
		conn, err := godbus.ConnectSystemBus()
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
		defer conn.RemoveSignal(sigCh)
		defer func() {
			if err := conn.RemoveMatchSignal(godbus.WithMatchObjectPath(transPath)); err != nil {
				logger.Debugf("failed to remove D-Bus match signal: %v", err)
			}
		}()
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
