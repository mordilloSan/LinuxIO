package packages

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/autoupdate"
	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

type UpdateDetail struct {
	PackageID string   `json:"package_id"`
	Summary   string   `json:"summary"`
	Version   string   `json:"version"`
	Issued    string   `json:"issued"`
	Changelog string   `json:"changelog"`
	Homepage  string   `json:"homepage,omitempty"`
	CVEs      []string `json:"cve"`
	Restart   uint32   `json:"restart"`
	State     uint32   `json:"state"`
	InfoEnum  uint32   `json:"info_enum"` // PackageKit info enum (severity/type): 0=Unknown, 1-30=various types
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

// packageKitTransactionTimeout bounds one PackageKit install, update or download transaction.
// autoupdate cannot import this package; keep its offlineDownloadTimeout equal.
const packageKitTransactionTimeout = 30 * time.Minute

const (
	pkInfoEnumUnknown = uint32(0)
	// PK_INFO_ENUM_LAST is a sentinel, not a real package info value.
	// Current PackageKit values occupy 0..30.
	pkInfoEnumLast = uint32(31)
)

func getAutoUpdates(ctx context.Context) (AutoUpdateState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b := autoupdate.SelectBackend(ctx)
	if b == nil {
		return AutoUpdateState{}, fmt.Errorf("no supported backend found")
	}
	if err := ctx.Err(); err != nil {
		return AutoUpdateState{}, err
	}
	return b.Read(ctx)
}

func setAutoUpdates(ctx context.Context, opts AutoUpdateOptions) (AutoUpdateState, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	b := autoupdate.SelectBackend(ctx)
	if b == nil {
		return AutoUpdateState{}, fmt.Errorf("no supported backend found")
	}
	if err := b.Apply(ctx, opts); err != nil {
		return AutoUpdateState{}, err
	}
	return b.Read(ctx)
}

func applyOfflineUpdates(ctx context.Context) (apischema.OfflineUpdatesResponse, error) {
	b := autoupdate.NewPkgKitBackendIfAvailable(ctx)
	if b == nil {
		return apischema.OfflineUpdatesResponse{}, fmt.Errorf("PackageKit not available")
	}
	report, err := preUpdateSnapshot(ctx)
	if msg := preUpdateSnapshotProblems(report, err); msg != "" {
		slog.Warn("btrfs pre-update snapshot problems; applying offline updates anyway", "problems", msg)
	}
	return apischema.OfflineUpdatesResponse{}, b.ApplyOfflineNow(ctx)
}

// --- Helpers ---

func extractCVEs(text string) []string {
	re := regexp.MustCompile(`CVE-\d{4}-\d+`)
	return re.FindAllString(text, -1)
}

// aptNoChangelog is what PackageKit's apt backend sends as the changelog when
// it has none: the repository publishes no changelog (Docker's, gh's, Ubuntu
// ESM's: apt knows where to look only for Debian's and Ubuntu's archives), it
// has not yet, or the download failed. The page says so in its own words.
const aptNoChangelog = "Changelog for this version is not yet available"

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

// packageIDVersion returns the version field of a PackageKit package ID
// ("name;version;arch;data").
func packageIDVersion(packageID string) string {
	parts := strings.Split(packageID, ";")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
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

func sanitizeInfoEnum(infoEnum uint32) uint32 {
	if infoEnum < pkInfoEnumLast {
		return infoEnum
	}
	return pkInfoEnumUnknown
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

func readPackageSignal(sig *dbusclient.Signal) (string, packageUpdateMeta, bool) {
	if sig.Name != pkgkit.TransactionIface+".Package" || len(sig.Body) <= 2 {
		return "", packageUpdateMeta{}, false
	}

	infoEnum, _ := sig.Body[0].(uint32)
	pkgID, _ := sig.Body[1].(string)
	summary, _ := sig.Body[2].(string)

	return pkgID, packageUpdateMeta{
		Summary:  summary,
		InfoEnum: sanitizeInfoEnum(infoEnum),
	}, true
}

// collectUpdatePackages reads a GetUpdates transaction to its end. One that
// PackageKit ends with ErrorCode, or with any exit but success, is a failed
// listing whatever it named first, so that an empty answer always means a
// list that was read.
func collectUpdatePackages(ctx context.Context, sigCh <-chan *dbusclient.Signal) ([]string, map[string]packageUpdateMeta, error) {
	var pkgIDs []string
	metaByPkg := make(map[string]packageUpdateMeta)

	for {
		select {
		case sig, ok := <-sigCh:
			if !ok {
				return nil, nil, pkgkit.ErrSignalsClosed
			}
			switch sig.Name {
			case pkgkit.TransactionIface + ".ErrorCode":
				return nil, nil, pkgkit.ErrorFromSignal(sig, "GetUpdates")
			case pkgkit.TransactionIface + ".Finished":
				if exit, failed := unsuccessfulExit(sig); failed {
					return nil, nil, fmt.Errorf("GetUpdates ended with PackageKit exit %d", exit)
				}
				return pkgIDs, metaByPkg, nil
			}

			pkgID, meta, ok := readPackageSignal(sig)
			if !ok {
				continue
			}

			pkgIDs = append(pkgIDs, pkgID)
			metaByPkg[pkgID] = meta
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
}

// unsuccessfulExit reads the exit of a Finished signal and whether it is a
// failure. PackageKit ends some transactions with no ErrorCode: one cancelled
// for a higher-priority transaction (a refresh), one skipped. Exit 0,
// unknown, counts as success, as an update's does.
func unsuccessfulExit(sig *dbusclient.Signal) (uint32, bool) {
	if len(sig.Body) == 0 {
		return 0, false
	}
	exit, _ := sig.Body[0].(uint32)
	return exit, exit > pkgkit.ExitSuccess
}

func buildBasicUpdates(pkgIDs []string, metaByPkg map[string]packageUpdateMeta) []UpdateDetail {
	updates := make([]UpdateDetail, 0, len(pkgIDs))
	for _, pkgID := range pkgIDs {
		version := packageIDVersion(pkgID)
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

	// The signal carries no version: its last field is the date the update
	// was last changed. The version is the package ID's.
	version := packageIDVersion(pkgID)

	issued, err := dbusclient.AsString(body[10])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid issued date for %q: %w", pkgID, err)
	}

	changelogRaw, err := dbusclient.AsString(body[8])
	if err != nil {
		return UpdateDetail{}, fmt.Errorf("invalid changelog for %q: %w", pkgID, err)
	}
	changelogRaw = strings.TrimSpace(changelogRaw)
	if changelogRaw == aptNoChangelog {
		changelogRaw = ""
	}
	// PackageKit's dnf backend sends no changelog, only the advisory's text
	// (the repository's updateinfo) as update_text.
	if changelogRaw == "" {
		changelogRaw, err = dbusclient.AsString(body[7])
		if err != nil {
			return UpdateDetail{}, fmt.Errorf("invalid update text for %q: %w", pkgID, err)
		}
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
		// The apt backend sends the entries newer than the installed version,
		// so an update over several versions shows all of them.
		Changelog: strings.TrimSpace(changelogRaw),
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
		case sig, ok := <-sigCh:
			if !ok {
				return nil, pkgkit.ErrSignalsClosed
			}
			if sig.Name == pkgkit.TransactionIface+".Finished" {
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

// FindUpdateByName returns the version of the update PackageKit lists for the
// package called name, as PackageKit gives it, or "" when it lists none. It
// reads the same list as the Updates page (GetUpdates on PackageKit's cache)
// and refreshes nothing; an error wrapping dbusclient.ErrPackageKitUnavailable
// means the host has no PackageKit.
func FindUpdateByName(ctx context.Context, name string) (string, error) {
	updates, err := getUpdatesBasic(ctx)
	if err != nil {
		return "", err
	}
	for _, update := range updates {
		if pkgName, _, _ := strings.Cut(update.PackageID, ";"); pkgName == name {
			return update.Version, nil
		}
	}
	return "", nil
}

// RefreshUpdateCache refreshes configured package repositories, matching the
// PackageKit operation behind apt update / dnf makecache.
func RefreshUpdateCache(ctx context.Context) error {
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)

		if err := trans.Call("RefreshCache", true); err != nil {
			return err
		}

		waitCtx, cancel := context.WithTimeout(session.Context(), 2*time.Minute)
		defer cancel()
		if err := trans.AwaitFinished(waitCtx, "RefreshCache"); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("timeout refreshing package cache")
			}
			return err
		}
		slog.Info("package cache refreshed")
		return nil
	})
}

// InstallByName resolves a package name and installs it, streaming
// PackageKit progress signals (percentage/status) to the supplied reporter so
// callers (e.g. capability install) can show a real progress bar. A nil
// reporter discards progress.
//
// PackageKit keeps its own package cache, apart from apt's or dnf's, and on a
// host where it was never refreshed it knows no package to install. A name it
// does not find is therefore looked up again after one cache refresh before
// it is reported as not found.
func InstallByName(ctx context.Context, name string, report pkgUpdateReporter) error {
	if report == nil {
		report = func(*PkgUpdateProgress) error { return nil }
	}
	id, err := resolveInstallablePackageID(ctx, name)
	if errors.Is(err, bridgeipc.ErrNotFound) {
		reportPkgUpdateProgress(report, &PkgUpdateProgress{Type: "status", Status: "Refreshing the package lists"})
		if refreshErr := RefreshUpdateCache(ctx); refreshErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			// One line, since the capability row shows the job's error as text.
			return fmt.Errorf("%w; refresh package lists: %w", err, refreshErr)
		}
		id, err = resolveInstallablePackageID(ctx, name)
	}
	if err != nil || id == "" {
		return err
	}
	return installPackage(ctx, id, report)
}

// resolveInstallablePackageID resolves a package name to the package ID that
// should be installed. It returns ("", nil) when the package is already
// installed (a no-op for callers) and an error wrapping ErrNotFound when no
// enabled repository has the name.
func resolveInstallablePackageID(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("package name is required")
	}

	notInstalled, err := resolvePackageIDs(ctx, name, pkgkitFilterNotInstalled|pkgkitFilterNewest)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	if len(notInstalled) > 0 {
		return notInstalled[0], nil
	}

	installed, err := resolvePackageIDs(ctx, name, pkgkitFilterInstalled|pkgkitFilterNewest)
	if err != nil {
		return "", fmt.Errorf("resolve %s (installed): %w", name, err)
	}
	if len(installed) > 0 {
		return "", nil // already installed
	}
	return "", fmt.Errorf("package %q %w in any enabled repository", name, bridgeipc.ErrNotFound)
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
		if pkErr, ok := errors.AsType[*pkgkit.Error](err); ok && pkErr.Code == pkgkit.ErrorPackageNotFound {
			return nil // a backend's way of saying no package matched
		}
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

		if callErr := trans.Call("GetUpdates", uint64(0)); callErr != nil {
			return callErr
		}

		waitCtx, cancel := context.WithTimeout(session.Context(), 15*time.Second)
		defer cancel()

		pkgIDs, metaByPkg, err := collectUpdatePackages(waitCtx, trans.Signals())
		if err != nil {
			return fmt.Errorf("collect updates: %w", err)
		}
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
		if err != nil || detail.Changelog != "" {
			return err
		}
		// Without notes, the page links to the project instead.
		detail.Homepage = packageHomepage(session, packageID)
		return nil
	})
	return detail, err
}

// packageHomepage is the homepage PackageKit's GetDetails gives for
// packageID (the package's Homepage or URL field) when it is an http or
// https address, and "" otherwise. It is an extra to the update's details, so
// a failure is logged and leaves the detail without one.
func packageHomepage(session pkgkit.ClientSession, packageID string) string {
	trans, err := session.CreateTransaction(4)
	if err != nil {
		warnNoHomepage(session.Context(), packageID, err)
		return ""
	}
	defer pkgkit.LogClose(session.Context(), trans)
	if err = trans.Call("GetDetails", []string{packageID}); err != nil {
		warnNoHomepage(session.Context(), packageID, err)
		return ""
	}
	// PackageKit reads the details from its cache; the request has already
	// waited for the update's details.
	waitCtx, cancel := context.WithTimeout(session.Context(), 5*time.Second)
	defer cancel()
	homepage := ""
	for {
		select {
		case sig, ok := <-trans.Signals():
			if !ok {
				return homepage
			}
			switch sig.Name {
			case pkgkit.TransactionIface + ".Details":
				if found := detailsHomepage(sig.Body, packageID); found != "" {
					homepage = found
				}
			case pkgkit.TransactionIface + ".ErrorCode":
				warnNoHomepage(session.Context(), packageID, pkgkit.ErrorFromSignal(sig, "GetDetails"))
			case pkgkit.TransactionIface + ".Finished":
				return homepage
			}
		case <-waitCtx.Done():
			warnNoHomepage(session.Context(), packageID, waitCtx.Err())
			return homepage
		}
	}
}

// warnNoHomepage logs why packageHomepage has none, unless the request was
// cancelled, which is the page's own doing (it closed or moved on).
func warnNoHomepage(ctx context.Context, packageID string, err error) {
	if ctx.Err() != nil {
		return
	}
	slog.Warn("could not read the package homepage", "package_id", packageID, "error", err)
}

// detailsHomepage reads the url of a Details signal (one a{sv} argument)
// about packageID, when it is an absolute http or https address.
func detailsHomepage(body []any, packageID string) string {
	if len(body) == 0 {
		return ""
	}
	data, ok := body[0].(map[string]dbusclient.Variant)
	if !ok {
		return ""
	}
	if id, _ := data["package-id"].Value().(string); id != packageID {
		return ""
	}
	raw, _ := data["url"].Value().(string)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
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
		slog.Debug("parsing dpkg update history")
		return parseDpkgLogs(ctx), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := os.Stat("/var/log/dnf.log"); err == nil {
		slog.Debug("parsing dnf update history")
		return parseDnfHistory(ctx, "/var/log/dnf.log"), nil
	}
	slog.Warn("no known package manager log found")
	return []apischema.UpdateHistoryRow{}, nil
}

func newDpkgLogPatterns() dpkgLogPatterns {
	return dpkgLogPatterns{
		install:   regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+(install|upgrade)\s+([^ ]+)\s+([^ ]+)(?:\s+([^ ]+))?`),
		configure: regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+configure\s+([^ ]+)\s+([^ ]+)`),
	}
}

// parseDpkgLogs reads dpkg.log plus all rotated variants (.1, .2.gz, …).
func parseDpkgLogs(ctx context.Context) []apischema.UpdateHistoryRow {
	historyMap := make(map[string][]apischema.UpgradeItem)
	pendingPackages := make(map[string]string)
	matches, _ := filepath.Glob("/var/log/dpkg.log*")
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	patterns := newDpkgLogPatterns()
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
	if m := patterns.install.FindStringSubmatch(line); len(m) == 6 {
		version := m[4]
		if m[5] != "" && m[4] != "<none>" {
			version = m[5] // lines carrying an old version read "<old> <new>"; a fresh install waits for configure
		}
		recordDpkgInstall(m[1], m[3], version, historyMap, pendingPackages)
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
	upgradeRe := regexp.MustCompile(`Upgrade:\s+(\S+)`)

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
		if len(date) >= 10 {
			date = date[:10] // dnf.log timestamps are ISO 8601; key by day like dpkg
		}

		if matches := upgradeRe.FindStringSubmatch(line); len(matches) > 1 {
			if name, version, ok := splitNEVRA(matches[1]); ok {
				historyMap[date] = append(historyMap[date], apischema.UpgradeItem{Package: name, Version: version})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error scanning DNF log", "component", "dbus", "subsystem", "updates", "path", logPath, "error", err)
	}

	return mapToSortedHistory(historyMap)
}

// splitNEVRA splits "name-version-release.arch" from the right, since names may contain hyphens.
func splitNEVRA(nevra string) (name, version string, ok bool) {
	before, _, ok := strings.CutLast(nevra, ".")
	if !ok {
		return "", "", false
	}
	rest := before
	before0, _, ok0 := strings.CutLast(rest, "-")
	if !ok0 {
		return "", "", false
	}
	verDash := strings.LastIndex(before0, "-")
	if verDash <= 0 {
		return "", "", false
	}
	return rest[:verDash], rest[verDash+1:], true
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

// installPackage installs a package ID and drains the PackageKit
// signal stream through the shared update-signal handlers so
// percentage/status progress reaches the reporter. A nil reporter discards
// progress.
func installPackage(ctx context.Context, packageID string, report pkgUpdateReporter) error {
	if report == nil {
		report = func(*PkgUpdateProgress) error { return nil }
	}
	return pkgkit.Run(ctx, pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
		trans, err := session.CreateTransaction(20)
		if err != nil {
			return err
		}
		defer pkgkit.LogClose(session.Context(), trans)

		termLog := openAptTermLog()
		defer termLog.Close()
		deadline := time.Now().Add(installTransactionTimeout)
		err = trans.Call("InstallPackages", uint64(0), []string{packageID})
		if err == nil {
			waitCtx, cancel := context.WithDeadline(session.Context(), deadline)
			defer cancel()
			err = awaitPackageUpdateSignals(waitCtx, trans.Signals(), termLog, report)
		}
		// Stopped by the cancel, not by the transaction's end: a call cut
		// short by it may have reached PackageKit too.
		if ctxErr := session.Context().Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			awaitCancelledInstall(session.Context(), trans, deadline, termLog, report)
		}
		return err
	})
}

// installTransactionTimeout bounds an install's transaction, from when the
// install asks PackageKit for it, cancelled or not. It is a test seam.
var installTransactionTimeout = packageKitTransactionTimeout

// transactionPollInterval is how often a cancelled install checks that
// PackageKit still has its transaction. It is a test seam.
var transactionPollInterval = 5 * time.Second

// errTransactionGone ends the wait for a transaction PackageKit no longer
// has and sent no Finished for, as when it crashed or restarted.
var errTransactionGone = errors.New("PackageKit no longer has the transaction")

// awaitCancelledInstall asks PackageKit to cancel the transaction of an
// install whose context was cancelled, then follows it until it ends, it is
// gone, or the install's deadline passes, on a context the cancel does not
// reach: what runs after the package step, such as a package hook letting go
// of a hold, must not run while PackageKit may still run the package's
// scripts. PackageKit cancels nothing while it changes the system, and its
// dnf5 backend never does, so the transaction may run to its end.
func awaitCancelledInstall(ctx context.Context, trans *pkgkit.Transaction, deadline time.Time, termLog *aptTermLog, report pkgUpdateReporter) {
	waitCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	cancelCtx, stop := context.WithTimeout(waitCtx, transactionCancelTimeout)
	err := trans.Cancel(cancelCtx)
	stop()
	switch {
	case errors.Is(err, pkgkit.ErrNeverStarted):
		return
	case err != nil:
		slog.Debug("PackageKit install transaction not canceled", "transaction", trans.Path(), "error", err)
	}
	reportPkgUpdateProgress(report, &PkgUpdateProgress{Type: "status", Status: "Waiting for PackageKit to end the transaction"})
	watchCtx, gone := context.WithCancelCause(waitCtx)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		watchTransaction(watchCtx, trans, gone)
	}()
	err = awaitPackageUpdateSignals(watchCtx, trans.Signals(), termLog, report)
	gone(nil)
	<-watched
	attrs := []any{"transaction", trans.Path(), "deadline", deadline, "error", err}
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		slog.Warn("PackageKit install transaction outlived the wait after a cancel", attrs...)
	case errors.Is(err, context.Canceled) && errors.Is(context.Cause(watchCtx), errTransactionGone):
		slog.Warn("PackageKit install transaction gone without a Finished after a cancel", attrs...)
	case errors.Is(err, pkgkit.ErrSignalsClosed):
		slog.Warn("PackageKit install transaction signals lost after a cancel", attrs...)
	default:
		slog.Debug("PackageKit install transaction failed after a cancel", attrs...)
	}
}

// watchTransaction ends ctx with errTransactionGone once PackageKit, having
// listed trans at one check, has not listed it at two checks in a row.
// PackageKit lists a transaction from its commit until it sends its Finished,
// so one still waiting for polkit is not listed yet and is never taken for
// gone; and that signal reaches LinuxIO on another connection than the list,
// so one check that misses it may race the Finished. A list that cannot be
// read breaks the row.
func watchTransaction(ctx context.Context, trans *pkgkit.Transaction, gone context.CancelCauseFunc) {
	ticker := time.NewTicker(transactionPollInterval)
	defer ticker.Stop()
	seen, missed := false, 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		running, err := trans.Running(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			missed = 0
			slog.Debug("PackageKit transaction list not read", "transaction", trans.Path(), "error", err)
		case running:
			seen, missed = true, 0
		case seen:
			missed++
			if missed == 2 {
				gone(errTransactionGone)
				return
			}
		}
	}
}
