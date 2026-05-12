package updates

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

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/updates/internal/autoupdate"
	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/updates/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/utils"
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
	b := autoupdate.SelectBackend()
	if b == nil {
		return AutoUpdateState{}, fmt.Errorf("no supported backend found")
	}
	ctx, cancel := context.WithTimeout(requireContext(ctx), 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return AutoUpdateState{}, err
	}
	return b.Read()
}

func setAutoUpdates(ctx context.Context, opts AutoUpdateOptions) (AutoUpdateState, error) {
	b := autoupdate.SelectBackend()
	if b == nil {
		return AutoUpdateState{}, fmt.Errorf("no supported backend found")
	}
	ctx, cancel := context.WithTimeout(requireContext(ctx), 8*time.Second)
	defer cancel()
	if err := b.Apply(ctx, opts); err != nil {
		return AutoUpdateState{}, err
	}
	return b.Read()
}

func applyOfflineUpdates(ctx context.Context) (any, error) {
	b := autoupdate.NewPkgKitBackendIfAvailable()
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

func isTransactionFinished(sig *godbus.Signal) bool {
	return sig == nil || sig.Name == pkgkit.TransactionIface+".Finished"
}

func readPackageSignal(sig *godbus.Signal) (string, packageUpdateMeta, bool) {
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

func collectUpdateDetails(ctx context.Context, sigCh <-chan *godbus.Signal, metaByPkg map[string]packageUpdateMeta) ([]UpdateDetail, error) {
	var details []UpdateDetail

	for {
		select {
		case sig := <-sigCh:
			if isTransactionFinished(sig) {
				return details, nil
			}
			if sig.Name != pkgkit.TransactionIface+".UpdateDetail" {
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

func GetUpdatesWithDetails(ctx context.Context) ([]UpdateDetail, error) {
	details, err := getUpdatesWithDetails(ctx)
	if err != nil {
		return nil, err
	}
	if details == nil {
		details = make([]UpdateDetail, 0)
	}
	return details, nil
}

func InstallPackage(ctx context.Context, packageID string) error {
	return installPackage(ctx, packageID)
}

// GetSingleUpdateDetail returns detailed info for a single package.
// Used for on-demand changelog fetching.
func GetSingleUpdateDetail(ctx context.Context, packageID string) (*UpdateDetail, error) {
	return getSingleUpdateDetail(ctx, packageID)
}

// --- Private Implementation ---

// getUpdatesBasic fetches available updates with basic info only (fast).
// Only calls GetUpdates, skips the slow GetUpdateDetail call.
func getUpdatesBasic(ctx context.Context) ([]UpdateDetail, error) {
	var updates []UpdateDetail
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{}, func(session pkgkit.Session) error {
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
func getSingleUpdateDetail(ctx context.Context, packageID string) (*UpdateDetail, error) {
	var detail *UpdateDetail
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{}, func(session pkgkit.Session) error {
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

func getUpdatesWithDetails(ctx context.Context) ([]UpdateDetail, error) {
	var details []UpdateDetail
	err := pkgkit.Run(ctx, pkgkit.OperationOptions{}, func(session pkgkit.Session) error {
		updatesTrans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), updatesTrans)

		if err = updatesTrans.Call("GetUpdates", uint64(0)); err != nil {
			return err
		}

		updatesCtx, cancelUpdates := context.WithTimeout(session.Context(), 15*time.Second)
		defer cancelUpdates()

		pkgIDs, metaByPkg := collectUpdatePackages(updatesCtx, updatesTrans.Signals())
		if len(pkgIDs) == 0 {
			return nil
		}

		detailsTrans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), detailsTrans)

		if err = detailsTrans.Call("GetUpdateDetail", pkgIDs); err != nil {
			return err
		}

		detailsCtx, cancelDetails := context.WithTimeout(session.Context(), 15*time.Second)
		defer cancelDetails()

		details, err = collectUpdateDetails(detailsCtx, detailsTrans.Signals(), metaByPkg)
		return err
	})
	return details, err
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
		slog.Debug("Parsing dpkg update history")
		return parseDpkgLogs(), nil
	}
	if _, err := os.Stat("/var/log/dnf.log"); err == nil {
		slog.Debug("Parsing dnf update history")
		return parseDnfHistory("/var/log/dnf.log"), nil
	}
	slog.Warn("No known package manager log found")
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
			slog.Warn("failed to open update log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
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
		slog.Error("failed to open DNF log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
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

func installPackage(ctx context.Context, packageID string) error {
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.Session) error {
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

func awaitPackageKitSignal(ctx context.Context, sigCh <-chan *godbus.Signal) error {
	if err := pkgkit.AwaitFinished(ctx, sigCh, ""); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timeout waiting for PackageKit to finish install")
		}
		return err
	}
	return nil
}

func requireContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
