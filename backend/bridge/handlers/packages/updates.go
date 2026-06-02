package packages

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/autoupdate"
	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
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
	AutoUpdateOptions = autoupdate.AutoUpdateOptions
	AutoUpdateState   = autoupdate.AutoUpdateState
)

type packageUpdateMeta struct {
	Summary  string
	InfoEnum uint32
}

func getAutoUpdates(ctx context.Context) (AutoUpdateState, error) {
	if ctx == nil {
		return AutoUpdateState{}, fmt.Errorf("nil context")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b := autoupdate.SelectBackend(ctx)
	if b == nil {
		return AutoUpdateState{}, fmt.Errorf("no supported backend found")
	}
	if err := ctx.Err(); err != nil {
		return AutoUpdateState{}, err
	}
	return b.Read()
}

func setAutoUpdates(ctx context.Context, opts AutoUpdateOptions) (AutoUpdateState, error) {
	if ctx == nil {
		return AutoUpdateState{}, fmt.Errorf("nil context")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	b := autoupdate.SelectBackend(ctx)
	if b == nil {
		return AutoUpdateState{}, fmt.Errorf("no supported backend found")
	}
	if err := b.Apply(ctx, opts); err != nil {
		return AutoUpdateState{}, err
	}
	return b.Read()
}

func applyOfflineUpdates(ctx context.Context) (any, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil context")
	}
	b := autoupdate.NewPkgKitBackendIfAvailable(ctx)
	if b == nil {
		return nil, fmt.Errorf("PackageKit not available")
	}
	return nil, b.ApplyOfflineNow(ctx)
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
	if strs, ok := iface.([]string); ok {
		return append([]string(nil), strs...)
	}
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
	slog.Debug("package has invalid PackageKit InfoEnum", "component", "dbus", "subsystem", "updates", "package", pkgID, "code", infoEnum)
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

func isTransactionFinished(sig *dbusclient.Signal) bool {
	return sig == nil || sig.Name == pkgkit.TransactionIface+".Finished"
}

func readPackageSignal(sig *dbusclient.Signal) (string, packageUpdateMeta, bool) {
	if sig.Name != pkgkit.TransactionIface+".Package" || len(sig.Body) <= 2 {
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

func collectUpdatePackages(ctx context.Context, sigCh <-chan *dbusclient.Signal) ([]string, map[string]packageUpdateMeta) {
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
	pkgID, err := dbusclient.AsString(body[0])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid pkgID: %w", err)
	}

	version, err := dbusclient.AsString(body[11])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid version for %q: %w", pkgID, err)
	}
	if version == "" {
		_, version = extractNameVersion(pkgID)
	}

	issued, err := dbusclient.AsString(body[10])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid issued date for %q: %w", pkgID, err)
	}

	changelogRaw, err := dbusclient.AsString(body[8])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid changelog for %q: %w", pkgID, err)
	}

	restart, err := dbusclient.AsUint32(body[6])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid restart flag for %q: %w", pkgID, err)
	}

	state, err := dbusclient.AsUint32(body[9])
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

func collectSingleUpdateDetail(ctx context.Context, sigCh <-chan *dbusclient.Signal, packageID string) (*UpdateDetail, error) {
	var detail *UpdateDetail

	for {
		select {
		case sig := <-sigCh:
			if isTransactionFinished(sig) {
				return finalizeSingleUpdateDetail(detail, packageID)
			}
			if sig.Name != pkgkit.TransactionIface+".UpdateDetail" {
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

// --- D-Bus Public Wrappers with Retry ---

// GetUpdatesBasic returns package updates with basic info only (fast).
// This skips the slow GetUpdateDetail D-Bus call.
func GetUpdatesBasic(ctx context.Context) ([]UpdateDetail, error) {
	updates, err := getUpdatesBasic(ctx)
	if err != nil {
		return nil, err
	}
	if updates == nil {
		updates = make([]UpdateDetail, 0)
	}
	return updates, nil
}

// InstallByName resolves a package by name via PackageKit and installs it.
// Returns nil (no-op) if the package is already installed. Returns an error
// if the package cannot be found in any enabled repository.
func InstallByName(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("package name is required")
	}

	notInstalled, err := resolvePackageIDs(ctx, name, pkgkitFilterNotInstalled|pkgkitFilterNewest)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", name, err)
	}
	if len(notInstalled) == 0 {
		installed, err := resolvePackageIDs(ctx, name, pkgkitFilterInstalled|pkgkitFilterNewest)
		if err != nil {
			return fmt.Errorf("resolve %s (installed): %w", name, err)
		}
		if len(installed) > 0 {
			return nil // already installed
		}
		return fmt.Errorf("package %q not found in any enabled repository", name)
	}
	return InstallPackage(ctx, notInstalled[0])
}

// PackageKit filter bitmask values. See
// https://www.freedesktop.org/software/PackageKit/gtk-doc/PackageKit-pk-enum.html#PkBitfield
const (
	pkgkitFilterInstalled    uint64 = 1 << 2
	pkgkitFilterNotInstalled uint64 = 1 << 3
	pkgkitFilterNewest       uint64 = 1 << 16
)

func resolvePackageIDs(ctx context.Context, name string, filter uint64) ([]string, error) {
	var ids []string
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)

		if err = trans.Call("Resolve", filter, []string{name}); err != nil {
			return err
		}

		waitCtx, cancel := context.WithTimeout(session.Context(), 30*time.Second)
		defer cancel()
		ids, err = pkgkit.CollectPackageIDs(waitCtx, trans.Signals(), "resolve "+name)
		return err
	})
	return ids, err
}

// getUpdatesBasic fetches available updates with basic info only (fast).
// Only calls GetUpdates, skips the slow GetUpdateDetail call.
func getUpdatesBasic(ctx context.Context) ([]UpdateDetail, error) {
	var updates []UpdateDetail
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)

		if err := trans.Call("GetUpdates", uint64(0)); err != nil {
			return err
		}

		waitCtx, cancel := context.WithTimeout(session.Context(), 15*time.Second)
		defer cancel()

		pkgIDs, metaByPkg := collectUpdatePackages(waitCtx, trans.Signals())
		updates = buildBasicUpdates(pkgIDs, metaByPkg)
		return nil
	})
	return updates, err
}

// getSingleUpdateDetail fetches detailed info for a single package.
// Used for on-demand changelog fetching.
func getSingleUpdateDetail(ctx context.Context, packageID string) (*UpdateDetail, error) {
	var detail *UpdateDetail
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)

		if err = trans.Call("GetUpdateDetail", []string{packageID}); err != nil {
			return err
		}

		waitCtx, cancel := context.WithTimeout(session.Context(), 10*time.Second)
		defer cancel()

		detail, err = collectSingleUpdateDetail(waitCtx, trans.Signals(), packageID)
		return err
	})
	return detail, err
}

// --- Update History (log parsing) ---

type dpkgLogPatterns struct {
	install   *regexp.Regexp
	configure *regexp.Regexp
}

func GetUpdateHistory(ctx context.Context) ([]apischema.UpdateHistoryRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat("/var/log/dpkg.log"); err == nil {
		slog.Debug("Parsing dpkg update history")
		return parseDpkgLogs(ctx), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat("/var/log/dnf.log"); err == nil {
		slog.Debug("Parsing dnf update history")
		return parseDnfHistory(ctx, "/var/log/dnf.log"), nil
	}
	slog.Warn("No known package manager log found")
	return []apischema.UpdateHistoryRow{}, nil
}

// parseDpkgLogs reads dpkg.log plus all rotated variants (.1, .2.gz, …).
func parseDpkgLogs(ctx context.Context) []apischema.UpdateHistoryRow {
	historyMap := make(map[string][]apischema.UpgradeItem)
	pendingPackages := make(map[string]string)
	matches, _ := filepath.Glob("/var/log/dpkg.log*")
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	patterns := dpkgLogPatterns{
		install:   regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+(install|upgrade)\s+([^ ]+)\s+([^ ]+)`),
		configure: regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+configure\s+([^ ]+)\s+([^ ]+)`),
	}
	for _, logPath := range matches {
		if err := ctx.Err(); err != nil {
			return mapToSortedHistory(historyMap)
		}
		parseDpkgLogFile(ctx, logPath, patterns, historyMap, pendingPackages)
	}

	return mapToSortedHistory(historyMap)
}

func parseDpkgLogFile(
	ctx context.Context,
	logPath string,
	patterns dpkgLogPatterns,
	historyMap map[string][]apischema.UpgradeItem,
	pendingPackages map[string]string,
) {
	reader, closer, err := openLogFile(logPath)
	if err != nil {
		slog.Warn("failed to open update log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
		return
	}
	defer closer()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return
		}
		applyDpkgLogLine(scanner.Text(), patterns, historyMap, pendingPackages)
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error scanning update log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
	}
}

func applyDpkgLogLine(line string, patterns dpkgLogPatterns, historyMap map[string][]apischema.UpgradeItem, pendingPackages map[string]string) {
	if m := patterns.install.FindStringSubmatch(line); len(m) == 5 {
		recordDpkgInstall(m[1], m[3], m[4], historyMap, pendingPackages)
		return
	}
	if m := patterns.configure.FindStringSubmatch(line); len(m) == 4 {
		recordDpkgConfigure(m[2], m[3], historyMap, pendingPackages)
	}
}

func recordDpkgInstall(date, pkg, version string, historyMap map[string][]apischema.UpgradeItem, pendingPackages map[string]string) {
	if version == "<none>" {
		pendingPackages[pkg] = date
		return
	}
	historyMap[date] = append(historyMap[date], apischema.UpgradeItem{Package: pkg, Version: version})
}

func recordDpkgConfigure(pkg, version string, historyMap map[string][]apischema.UpgradeItem, pendingPackages map[string]string) {
	origDate, exists := pendingPackages[pkg]
	if !exists {
		return
	}
	historyMap[origDate] = append(historyMap[origDate], apischema.UpgradeItem{Package: pkg, Version: version})
	delete(pendingPackages, pkg)
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

func parseDnfHistory(ctx context.Context, logPath string) []apischema.UpdateHistoryRow {
	if err := ctx.Err(); err != nil {
		return nil
	}
	file, err := os.Open(logPath)
	if err != nil {
		slog.Error("failed to open DNF log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	upgradeRe := regexp.MustCompile(`Upgrade:\s+([^\s-]+)-([^-]+-[^\s]+)`)

	historyMap := make(map[string][]apischema.UpgradeItem)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return mapToSortedHistory(historyMap)
		}
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 1 {
			continue
		}
		date := parts[0]

		if matches := upgradeRe.FindStringSubmatch(line); len(matches) > 2 {
			historyMap[date] = append(historyMap[date], apischema.UpgradeItem{
				Package: matches[1],
				Version: matches[2],
			})
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error scanning DNF log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
	}

	return mapToSortedHistory(historyMap)
}

func mapToSortedHistory(historyMap map[string][]apischema.UpgradeItem) []apischema.UpdateHistoryRow {
	var dates []string
	for date := range historyMap {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	var history []apischema.UpdateHistoryRow
	for _, date := range dates {
		history = append(history, apischema.UpdateHistoryRow{
			Date:     date,
			Upgrades: historyMap[date],
		})
	}
	return history
}

// InstallPackage installs a specific PackageKit package by package ID
// (typically obtained from a previous Resolve or GetUpdates response).
func InstallPackage(ctx context.Context, packageID string) error {
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)

		if err := trans.Call("InstallPackages", uint64(0), []string{packageID}); err != nil {
			return err
		}

		waitCtx, cancel := context.WithTimeout(session.Context(), 120*time.Second)
		defer cancel()
		return awaitPackageKitSignal(waitCtx, trans.Signals())
	})
}

func awaitPackageKitSignal(ctx context.Context, sigCh <-chan *dbusclient.Signal) error {
	if err := pkgkit.AwaitFinished(ctx, sigCh, ""); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timeout waiting for PackageKit to finish install")
		}
		return err
	}
	return nil
}
