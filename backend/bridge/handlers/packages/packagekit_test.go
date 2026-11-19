package packages

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient/testdbus"
)

type fakePackage struct {
	info    uint32
	id      string
	summary string
}

type fakePackageKit struct {
	t           *testing.T
	conn        *godbus.Conn
	prepared    bool
	termLogPath string

	mu              sync.Mutex
	nextTransaction int
	updates         []fakePackage
	details         map[string][]any
	installed       []string
	updated         []string
	updateCalls     [][]string
	updateErrors    map[string]string
	updateHangs     map[string]bool
	updateStarted   chan string
	resolvable      map[string]fakeResolvable
	resolveCalls    int
	resolveNotFound bool
	refreshes       int
	refreshErr      string
	refreshHangs    bool
	refreshStarted  chan struct{}
	offlineTriggers []string
	triggerErr      *godbus.Error
	// running are the transactions GetTransactionList answers, as PackageKit
	// lists those it still runs; listed receives a value at each answer.
	running []godbus.ObjectPath
	listed  chan struct{}
	// history is what GetOldTransactions sends, newest first.
	history []fakeOldTransaction
	cancels []godbus.ObjectPath
	// refuseCancel answers Cancel as PackageKit does while dpkg runs.
	refuseCancel bool
	// exits ends a package's update, or an install, with that Finished exit
	// and no ErrorCode.
	exits map[string]uint32
}

// fakeOldTransaction is one entry of PackageKit's transaction history.
type fakeOldTransaction struct {
	path      godbus.ObjectPath
	succeeded bool
	role      uint32
	// duration is 0 for a transaction PackageKit recorded starting and never
	// recorded ending.
	duration uint32
}

// fakeResolvable is a package Resolve finds by name. coldCache hides it until
// a RefreshCache has succeeded, as PackageKit's own cache does on a host where
// it was never refreshed.
type fakeResolvable struct {
	id        string
	installed bool
	coldCache bool
}

type fakeTransaction struct {
	service *fakePackageKit
	path    godbus.ObjectPath
}

func setupFakePackageKit(t *testing.T, prepared bool) *fakePackageKit {
	t.Helper()

	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	conn := bus.OwnName(t, dbusclient.PackageKitBusName)
	// Lines already in term.log belong to earlier transactions and must not be relayed.
	termLogPath := filepath.Join(t.TempDir(), "term.log")
	if err := os.WriteFile(termLogPath, []byte("Setting up earlier (1.0) ...\n"), 0o600); err != nil {
		t.Fatalf("write term.log: %v", err)
	}
	previousTermLogPath := aptTermLogPath
	aptTermLogPath = termLogPath
	t.Cleanup(func() { aptTermLogPath = previousTermLogPath })
	service := &fakePackageKit{
		t:              t,
		conn:           conn,
		prepared:       prepared,
		termLogPath:    termLogPath,
		details:        make(map[string][]any),
		updateErrors:   make(map[string]string),
		updateHangs:    make(map[string]bool),
		updateStarted:  make(chan string, 16),
		resolvable:     make(map[string]fakeResolvable),
		refreshStarted: make(chan struct{}, 16),
		listed:         make(chan struct{}, 16),
		exits:          make(map[string]uint32),
	}

	path := godbus.ObjectPath(dbusclient.PackageKitPath)
	if err := conn.Export(service, path, dbusclient.PackageKitIface); err != nil {
		t.Fatalf("export PackageKit root: %v", err)
	}
	if err := conn.Export(service, path, dbusclient.PackageKitOfflineIface); err != nil {
		t.Fatalf("export PackageKit offline: %v", err)
	}
	if _, err := prop.Export(conn, path, prop.Map{
		dbusclient.PackageKitOfflineIface: {
			"UpdatePrepared": {Value: prepared, Emit: prop.EmitTrue},
		},
	}); err != nil {
		t.Fatalf("export PackageKit properties: %v", err)
	}
	return service
}

func (s *fakePackageKit) CreateTransaction() (godbus.ObjectPath, *godbus.Error) {
	s.mu.Lock()
	s.nextTransaction++
	path := godbus.ObjectPath("/org/freedesktop/PackageKit/transactions/" + string(rune('a'+s.nextTransaction)))
	s.mu.Unlock()

	tx := &fakeTransaction{service: s, path: path}
	if err := s.conn.Export(tx, path, dbusclient.PackageKitTransactionIface); err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return path, nil
}

func (s *fakePackageKit) GetTransactionList() ([]godbus.ObjectPath, *godbus.Error) {
	s.mu.Lock()
	running := slices.Clone(s.running)
	s.mu.Unlock()
	s.listed <- struct{}{}
	return running, nil
}

// runTransaction is a transaction of role that PackageKit runs at path for a
// client that is gone, such as a job host that died.
func (s *fakePackageKit) runTransaction(path godbus.ObjectPath, role uint32) *fakeTransaction {
	s.t.Helper()
	tx := &fakeTransaction{service: s, path: path}
	if err := s.conn.Export(tx, path, dbusclient.PackageKitTransactionIface); err != nil {
		s.t.Fatalf("export transaction %s: %v", path, err)
	}
	if _, err := prop.Export(s.conn, path, prop.Map{
		dbusclient.PackageKitTransactionIface: {"Role": {Value: role, Emit: prop.EmitFalse}},
	}); err != nil {
		s.t.Fatalf("export transaction %s properties: %v", path, err)
	}
	s.mu.Lock()
	s.running = append(s.running, path)
	s.mu.Unlock()
	return tx
}

// listGone lists path as running while its object is already gone, as for a
// transaction that ended and was dropped between two calls.
func (s *fakePackageKit) listGone(path godbus.ObjectPath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = append(s.running, path)
}

func (s *fakePackageKit) setExit(packageID string, exit uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exits[packageID] = exit
}

// finish ends a transaction runTransaction started, with PackageKit's error
// when message is not empty.
func (tx *fakeTransaction) finish(message string) {
	tx.service.mu.Lock()
	tx.service.running = slices.DeleteFunc(tx.service.running, func(p godbus.ObjectPath) bool { return p == tx.path })
	tx.service.mu.Unlock()
	if message != "" {
		tx.emit("ErrorCode", uint32(1), message)
		tx.emit("Finished", uint32(2), uint32(0))
		return
	}
	tx.emit("Package", uint32(11), "picked;1.0;x86_64;repo", "Picked up")
	tx.emit("Finished", uint32(1), uint32(0))
}

func (s *fakePackageKit) setHistory(entries ...fakeOldTransaction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = entries
}

func (s *fakePackageKit) canceled() []godbus.ObjectPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.cancels)
}

func (s *fakePackageKit) Trigger(action string) *godbus.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.triggerErr != nil {
		return s.triggerErr
	}
	s.offlineTriggers = append(s.offlineTriggers, action)
	return nil
}

func (s *fakePackageKit) setUpdates(updates []fakePackage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append([]fakePackage(nil), updates...)
}

func (s *fakePackageKit) snapshotUpdates() []fakePackage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakePackage(nil), s.updates...)
}

func (s *fakePackageKit) setDetail(packageID string, body []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.details[packageID] = copyDetailBody(body)
}

func (s *fakePackageKit) snapshotDetail(packageID string) ([]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.details[packageID]
	if !ok {
		return nil, false
	}
	return copyDetailBody(body), true
}

func (s *fakePackageKit) setTriggerErr(err *godbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggerErr = err
}

func (s *fakePackageKit) installedPackages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.installed...)
}

func (s *fakePackageKit) updatedPackages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.updated...)
}

func (s *fakePackageKit) updatePackageCalls() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	updates := make([][]string, len(s.updateCalls))
	for index, call := range s.updateCalls {
		updates[index] = append([]string(nil), call...)
	}
	return updates
}

func (s *fakePackageKit) setUpdateError(packageID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateErrors[packageID] = message
}

func (s *fakePackageKit) setUpdateHang(packageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateHangs[packageID] = true
}

func (s *fakePackageKit) refreshCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshes
}

func (s *fakePackageKit) setResolvable(name string, pkg fakeResolvable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolvable[name] = pkg
}

// answerUnknownNamesWithError makes Resolve end a lookup that matches nothing
// with PackageKit's package-not-found error, as some backends do, instead of
// an empty result.
func (s *fakePackageKit) answerUnknownNamesWithError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveNotFound = true
}

func (s *fakePackageKit) resolveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveCalls
}

func (s *fakePackageKit) setRefreshError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshErr = message
}

func (s *fakePackageKit) setRefreshHang() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshHangs = true
}

func (s *fakePackageKit) triggers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.offlineTriggers...)
}

// RefreshCache counts only refreshes that succeed, the ones that warm the cache.
func (tx *fakeTransaction) RefreshCache(force bool) *godbus.Error {
	tx.service.mu.Lock()
	refreshErr, hangs := tx.service.refreshErr, tx.service.refreshHangs
	if force && refreshErr == "" && !hangs {
		tx.service.refreshes++
	}
	tx.service.mu.Unlock()
	tx.service.refreshStarted <- struct{}{}
	if hangs {
		return nil
	}
	tx.emitLater(func() {
		if refreshErr != "" {
			tx.emit("ErrorCode", uint32(2), refreshErr) // PK_ERROR_ENUM_NO_NETWORK
			tx.emit("Finished", uint32(1), uint32(0))
			return
		}
		tx.emit("Finished", uint32(0), uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) Resolve(filter uint64, names []string) *godbus.Error {
	s := tx.service
	s.mu.Lock()
	s.resolveCalls++
	wantInstalled := filter&pkgkitFilterInstalled != 0
	var found []fakeResolvable
	for _, name := range names {
		pkg, ok := s.resolvable[name]
		if ok && pkg.installed == wantInstalled && (!pkg.coldCache || s.refreshes > 0) {
			found = append(found, pkg)
		}
	}
	notFoundErr := s.resolveNotFound
	s.mu.Unlock()
	tx.emitLater(func() {
		if len(found) == 0 && notFoundErr {
			tx.emit("ErrorCode", uint32(8), "Package not found: "+strings.Join(names, ", ")) // PK_ERROR_ENUM_PACKAGE_NOT_FOUND
			tx.emit("Finished", uint32(1), uint32(0))
			return
		}
		for _, pkg := range found {
			info := uint32(2) // Available
			if pkg.installed {
				info = 1 // Installed
			}
			tx.emit("Package", info, pkg.id, "Demo package")
		}
		tx.emit("Finished", uint32(0), uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) GetUpdates(filters uint64) *godbus.Error {
	updates := tx.service.snapshotUpdates()
	tx.emitLater(func() {
		for _, pkg := range updates {
			tx.emit("Package", pkg.info, pkg.id, pkg.summary)
		}
		tx.emit("Finished", uint32(0), uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) GetUpdateDetail(packageIDs []string) *godbus.Error {
	var details [][]any
	if body, ok := tx.service.snapshotDetail("wrong;0;x86_64;repo"); ok {
		details = append(details, body)
	}
	for _, id := range packageIDs {
		if body, ok := tx.service.snapshotDetail(id); ok {
			details = append(details, body)
		}
	}
	tx.emitLater(func() {
		for _, body := range details {
			tx.emit("UpdateDetail", body...)
		}
		tx.emit("Finished", uint32(0), uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) InstallPackages(flags uint64, packageIDs []string) *godbus.Error {
	tx.service.mu.Lock()
	tx.service.installed = append(tx.service.installed, packageIDs...)
	exit := tx.service.exits[packageIDs[0]]
	tx.service.mu.Unlock()
	tx.emitLater(func() {
		tx.emit("Finished", exit, uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) UpdatePackages(flags uint64, packageIDs []string) *godbus.Error {
	tx.service.mu.Lock()
	tx.service.updated = append(tx.service.updated, packageIDs...)
	tx.service.updateCalls = append(tx.service.updateCalls, append([]string(nil), packageIDs...))
	packageID := packageIDs[0]
	updateError := tx.service.updateErrors[packageID]
	updateHangs := tx.service.updateHangs[packageID]
	exit, exits := tx.service.exits[packageID]
	tx.service.mu.Unlock()
	tx.service.updateStarted <- packageID
	if updateHangs {
		return nil
	}
	tx.emitLater(func() {
		if updateError != "" {
			tx.emit("ErrorCode", uint32(1), updateError)
			tx.emit("Finished", uint32(1), uint32(0))
			return
		}
		if exits {
			tx.emit("Finished", exit, uint32(0))
			return
		}
		tx.emit("Package", uint32(11), packageID, "Kernel update")
		_ = tx.service.conn.Emit(
			tx.path,
			dbusclient.PropertiesIface+".PropertiesChanged",
			dbusclient.PackageKitTransactionIface,
			map[string]godbus.Variant{
				"Status":     godbus.MakeVariant(uint32(10)),
				"Percentage": godbus.MakeVariant(uint32(42)),
			},
			[]string{},
		)
		tx.emit("ItemProgress", packageID, uint32(10), uint32(50))
		tx.service.appendTermLog("Setting up " + packageID + " ...\n")
		tx.emit("Finished", uint32(0), uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) GetOldTransactions(number uint32) *godbus.Error {
	tx.service.mu.Lock()
	history := slices.Clone(tx.service.history)
	tx.service.mu.Unlock()
	if number > 0 && int(number) < len(history) {
		history = history[:number]
	}
	tx.emitLater(func() {
		for _, old := range history {
			tx.emit("Transaction", old.path, "2026-11-18T10:00:00Z", old.succeeded, old.role, old.duration, "", uint32(0), "linuxio")
		}
		tx.emit("Finished", uint32(1), uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) Cancel() *godbus.Error {
	tx.service.mu.Lock()
	defer tx.service.mu.Unlock()
	tx.service.cancels = append(tx.service.cancels, tx.path)
	if tx.service.refuseCancel {
		return godbus.NewError("org.freedesktop.PackageKit.Transaction.CannotCancel", []any{"cannot cancel while dpkg runs"})
	}
	return nil
}

func (s *fakePackageKit) appendTermLog(text string) {
	file, err := os.OpenFile(s.termLogPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		s.t.Errorf("open term.log: %v", err)
		return
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		s.t.Errorf("append term.log: %v", err)
	}
}

func (tx *fakeTransaction) emit(member string, values ...any) {
	if err := tx.service.conn.Emit(tx.path, dbusclient.PackageKitTransactionIface+"."+member, values...); err != nil {
		tx.service.t.Errorf("emit %s: %v", member, err)
	}
}

func (tx *fakeTransaction) emitLater(fn func()) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		fn()
	}()
}

func detailBody(packageID, version string) []any {
	return []any{
		packageID,
		"", "", "", "",
		[]string{"CVE-2026-0001"},
		uint32(1),
		"",
		"demo (1.0) stable; urgency=medium\n\n  * fix CVE-2026-0002\n\n -- Demo <demo@example.com>  Tue, 12 May 2026 12:00:00 +0000",
		uint32(2),
		"2026-05-12T12:00:00Z",
		version,
	}
}

func copyDetailBody(body []any) []any {
	out := append([]any(nil), body...)
	for i, value := range out {
		switch v := value.(type) {
		case []string:
			out[i] = append([]string(nil), v...)
		case []any:
			out[i] = append([]any(nil), v...)
		}
	}
	return out
}

func TestGetUpdatesBasicCollectsPackageSignals(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setUpdates([]fakePackage{
		{info: 11, id: "demo;1.2.3;x86_64;repo", summary: "Demo package"},
	})

	got, err := GetUpdatesBasic(context.Background())
	if err != nil {
		t.Fatalf("GetUpdatesBasic: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("updates = %d, want 1", len(got))
	}
	if got[0].PackageID != "demo;1.2.3;x86_64;repo" || got[0].Summary != "Demo package" || got[0].Version != "1.2.3" || got[0].InfoEnum != 11 {
		t.Fatalf("unexpected update: %#v", got[0])
	}
}

func TestGetUpdatesBasicSanitizesOutOfRangePackageInfo(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setUpdates([]fakePackage{
		{info: 30, id: "valid;1.0;x86_64;repo", summary: "Known package info"},
		{info: 31, id: "last;1.0;x86_64;repo", summary: "Sentinel package info"},
		{info: 999, id: "future;1.0;x86_64;repo", summary: "Future package info"},
	})

	got, err := GetUpdatesBasic(context.Background())
	if err != nil {
		t.Fatalf("GetUpdatesBasic: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("updates = %d, want 3", len(got))
	}
	if got[0].InfoEnum != 30 {
		t.Fatalf("valid info enum = %d, want 30", got[0].InfoEnum)
	}
	if got[1].InfoEnum != 0 || got[2].InfoEnum != 0 {
		t.Fatalf("out-of-range info enums = %d, %d; want 0, 0", got[1].InfoEnum, got[2].InfoEnum)
	}
}

func TestRefreshUpdateCacheCallsRefreshCache(t *testing.T) {
	service := setupFakePackageKit(t, false)

	if err := RefreshUpdateCache(context.Background()); err != nil {
		t.Fatalf("RefreshUpdateCache: %v", err)
	}
	if got := service.refreshCount(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
}

func TestGetSingleUpdateDetailIgnoresNonMatchingDetail(t *testing.T) {
	service := setupFakePackageKit(t, false)
	const packageID = "demo;1.2.3;x86_64;repo"
	service.setDetail("wrong;0;x86_64;repo", detailBody("wrong;0;x86_64;repo", "0"))
	service.setDetail(packageID, detailBody(packageID, "1.2.3"))

	got, err := getSingleUpdateDetail(context.Background(), packageID)
	if err != nil {
		t.Fatalf("getSingleUpdateDetail: %v", err)
	}
	if got.PackageID != packageID || got.Version != "1.2.3" {
		t.Fatalf("unexpected detail: %#v", got)
	}
	if !slices.Contains(got.CVEs, "CVE-2026-0001") || !slices.Contains(got.CVEs, "CVE-2026-0002") {
		t.Fatalf("CVEs = %#v, want merged CVEs", got.CVEs)
	}
}

func TestInstallPackageWithProgressCallsInstallPackagesAndWaits(t *testing.T) {
	service := setupFakePackageKit(t, false)

	if err := installPackageWithProgress(context.Background(), "demo;1.2.3;x86_64;repo", nil); err != nil {
		t.Fatalf("installPackageWithProgress: %v", err)
	}
	if got := service.installedPackages(); !slices.Equal(got, []string{"demo;1.2.3;x86_64;repo"}) {
		t.Fatalf("installed = %#v", got)
	}
}

const coldPackageID = "zram-generator;1.2.1-3.fc44;x86_64;fedora"

func TestInstallByNameRefreshesTheCacheOnceWhenResolveFindsNothing(t *testing.T) {
	for name, answerWithError := range map[string]bool{"empty result": false, "package-not-found error": true} {
		t.Run(name, func(t *testing.T) {
			service := setupFakePackageKit(t, false)
			service.setResolvable("zram-generator", fakeResolvable{id: coldPackageID, coldCache: true})
			if answerWithError {
				service.answerUnknownNamesWithError()
			}
			var statuses []string
			report := func(p *PkgUpdateProgress) error {
				if p.Type == "status" {
					statuses = append(statuses, p.Status)
				}
				return nil
			}

			if err := InstallByNameWithProgress(context.Background(), "zram-generator", report); err != nil {
				t.Fatalf("InstallByNameWithProgress: %v", err)
			}
			if got := service.refreshCount(); got != 1 {
				t.Fatalf("refresh count = %d, want 1", got)
			}
			if got := service.installedPackages(); !slices.Equal(got, []string{coldPackageID}) {
				t.Fatalf("installed = %#v, want the package found after the refresh", got)
			}
			if !slices.Contains(statuses, "Refreshing the package lists") {
				t.Fatalf("status frames = %#v, want the refresh reported", statuses)
			}
		})
	}
}

func TestInstallByNameDoesNotRefreshWhenResolveFindsThePackage(t *testing.T) {
	tests := map[string]struct {
		pkg             fakeResolvable
		answerWithError bool
		wantInstalled   []string
	}{
		"available":                           {pkg: fakeResolvable{id: coldPackageID}, wantInstalled: []string{coldPackageID}},
		"already installed":                   {pkg: fakeResolvable{id: coldPackageID, installed: true}},
		"installed, not-found for ~installed": {pkg: fakeResolvable{id: coldPackageID, installed: true}, answerWithError: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			service := setupFakePackageKit(t, false)
			service.setResolvable("zram-generator", tc.pkg)
			if tc.answerWithError {
				service.answerUnknownNamesWithError()
			}

			if err := InstallByNameWithProgress(context.Background(), "zram-generator", func(*PkgUpdateProgress) error { return nil }); err != nil {
				t.Fatalf("InstallByNameWithProgress: %v", err)
			}
			if got := service.refreshCount(); got != 0 {
				t.Fatalf("refresh count = %d, want no refresh", got)
			}
			if got := service.installedPackages(); !slices.Equal(got, tc.wantInstalled) {
				t.Fatalf("installed = %#v, want %#v", got, tc.wantInstalled)
			}
		})
	}
}

func TestInstallByNameReportsNotFoundAfterOneRefresh(t *testing.T) {
	service := setupFakePackageKit(t, false)

	err := InstallByNameWithProgress(context.Background(), "no-such-package", nil)
	if !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if want := `package "no-such-package" not found in any enabled repository`; err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	if got := service.refreshCount(); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
	if got := service.resolveCount(); got != 4 {
		t.Fatalf("resolve calls = %d, want both lookups before and after the refresh", got)
	}
	if got := service.installedPackages(); len(got) != 0 {
		t.Fatalf("installed = %#v, want nothing", got)
	}
}

func TestInstallByNameKeepsNotFoundWhenTheRefreshFails(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setResolvable("zram-generator", fakeResolvable{id: coldPackageID, coldCache: true})
	service.setRefreshError("Cannot download repomd.xml")

	err := InstallByNameWithProgress(context.Background(), "zram-generator", func(*PkgUpdateProgress) error { return nil })
	if !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if want := `package "zram-generator" not found in any enabled repository; refresh package lists: RefreshCache error: Cannot download repomd.xml`; err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	if got := service.resolveCount(); got != 2 {
		t.Fatalf("resolve calls = %d, want no lookup after the failed refresh", got)
	}
	if got := service.installedPackages(); len(got) != 0 {
		t.Fatalf("installed = %#v, want nothing", got)
	}
}

func TestInstallByNameStopsWhenCancelledDuringTheRefresh(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setResolvable("zram-generator", fakeResolvable{id: coldPackageID, coldCache: true})
	service.setRefreshHang()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- InstallByNameWithProgress(ctx, "zram-generator", func(*PkgUpdateProgress) error { return nil })
	}()

	select {
	case <-service.refreshStarted:
	case err := <-errCh:
		t.Fatalf("InstallByNameWithProgress returned %v before refreshing", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the cache refresh to start")
	}
	cancel()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("InstallByNameWithProgress did not return after cancellation")
	}
	if !errors.Is(err, context.Canceled) || errors.Is(err, bridgeipc.ErrNotFound) {
		t.Fatalf("error = %v, want cancellation and no not-found verdict", err)
	}
	if got := service.resolveCount(); got != 2 {
		t.Fatalf("resolve calls = %d, want no lookup after the cancelled refresh", got)
	}
	if got := service.installedPackages(); len(got) != 0 {
		t.Fatalf("installed = %#v, want nothing", got)
	}
}

func TestUpdatePackagesWithProgressReportsSignals(t *testing.T) {
	service := setupFakePackageKit(t, false)
	var progress []PkgUpdateProgress
	report := func(p *PkgUpdateProgress) error {
		progress = append(progress, *p)
		return nil
	}

	if err := updatePackagesWithProgress(context.Background(), []string{"demo;1.2.3;x86_64;repo"}, report); err != nil {
		t.Fatalf("updatePackagesWithProgress: %v", err)
	}
	if got := service.updatedPackages(); !slices.Equal(got, []string{"demo;1.2.3;x86_64;repo"}) {
		t.Fatalf("updated = %#v", got)
	}
	if !hasProgressType(progress, "package") || !hasProgressType(progress, "status") || !hasProgressType(progress, "percentage") || !hasProgressType(progress, "item_progress") {
		t.Fatalf("progress frames = %#v", progress)
	}
	var output []string
	for _, frame := range progress {
		if frame.Type == "output" {
			output = append(output, frame.Message)
		}
	}
	if want := []string{"Setting up demo;1.2.3;x86_64;repo ..."}; !slices.Equal(output, want) {
		t.Fatalf("output frames = %#v, want only this transaction's term.log lines %#v", output, want)
	}
}

func TestUpdatePackagesWithProgressContinuesAfterPackageFailure(t *testing.T) {
	service := setupFakePackageKit(t, false)
	packageIDs := []string{
		"first;1.0;x86_64;repo",
		"blocked;1.0;x86_64;repo",
		"last;1.0;x86_64;repo",
	}
	service.setUpdateError(packageIDs[1], "not available yet")
	var progress []PkgUpdateProgress
	report := func(p *PkgUpdateProgress) error {
		progress = append(progress, *p)
		return nil
	}

	err := updatePackagesWithProgress(context.Background(), packageIDs, report)
	if err == nil || !strings.Contains(err.Error(), packageIDs[1]) || !strings.Contains(err.Error(), "not available yet") {
		t.Fatalf("error = %v, want aggregated failed package", err)
	}
	if !strings.Contains(err.Error(), "updated 2 of 3 packages; 1 failed") {
		t.Fatalf("error = %q, want partial-success counts", err)
	}
	if got := service.updatePackageCalls(); !slices.EqualFunc(got, [][]string{{packageIDs[0]}, {packageIDs[1]}, {packageIDs[2]}}, slices.Equal) {
		t.Fatalf("UpdatePackages calls = %#v, want one call per package", got)
	}

	var percentages []uint32
	var continued bool
	var completedPackages int
	for _, frame := range progress {
		if frame.Percentage != nil {
			percentages = append(percentages, *frame.Percentage)
		}
		if strings.Contains(frame.Message, "Continuing with remaining updates") {
			continued = true
		}
		if strings.HasPrefix(frame.Status, "Completed package ") {
			completedPackages++
		}
	}
	if !continued {
		t.Fatalf("progress frames = %#v, want failure continuation message", progress)
	}
	if completedPackages != 2 {
		t.Fatalf("completed package frames = %d, want 2", completedPackages)
	}
	for index := 1; index < len(percentages); index++ {
		if percentages[index] < percentages[index-1] {
			t.Fatalf("percentages = %#v, want monotonic progress", percentages)
		}
	}
	if len(percentages) == 0 || percentages[len(percentages)-1] != 100 {
		t.Fatalf("percentages = %#v, want final 100", percentages)
	}
}

func TestUpdatePackagesWithProgressPreservesSinglePackageError(t *testing.T) {
	service := setupFakePackageKit(t, false)
	const packageID = "blocked;1.0;x86_64;repo"
	service.setUpdateError(packageID, "not available yet")

	err := updatePackagesWithProgress(
		context.Background(),
		[]string{packageID},
		func(*PkgUpdateProgress) error { return nil },
	)
	if err == nil || err.Error() != "PackageKit error 1: not available yet" {
		t.Fatalf("error = %v, want original single-package error", err)
	}
}

func TestUpdatePackagesWithProgressStopsOnCancellation(t *testing.T) {
	service := setupFakePackageKit(t, false)
	packageIDs := []string{
		"first;1.0;x86_64;repo",
		"last;1.0;x86_64;repo",
	}
	service.setUpdateHang(packageIDs[0])
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- updatePackagesWithProgress(ctx, packageIDs, func(*PkgUpdateProgress) error { return nil })
	}()

	select {
	case started := <-service.updateStarted:
		if started != packageIDs[0] {
			t.Fatalf("started package = %q, want %q", started, packageIDs[0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first package update to start")
	}
	cancel()
	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if got := service.updatePackageCalls(); !slices.EqualFunc(got, [][]string{{packageIDs[0]}}, slices.Equal) {
		t.Fatalf("UpdatePackages calls = %#v, want cancellation to stop the batch", got)
	}
	// The cancel reaches PackageKit's transaction too.
	if got := service.canceled(); len(got) != 1 {
		t.Fatalf("PackageKit Cancel calls = %v, want one on the running transaction", got)
	}
}

func TestPackageInfoNameMapsPackageKitInfoCodes(t *testing.T) {
	tests := map[uint32]string{
		18: "Finished",
		22: "Decompressing",
	}
	for code, want := range tests {
		if got := packageInfoName(code); got != want {
			t.Fatalf("packageInfoName(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestPkgUpdateProgressEnvelopeUsesItemPercentageFallback(t *testing.T) {
	itemPercentage := uint32(42)
	detail := PkgUpdateProgress{
		Type:    "item_progress",
		Status:  "Installing",
		ItemPct: &itemPercentage,
	}
	progress := detail.ProgressEnvelope()
	if progress.Percentage == nil || *progress.Percentage != 42 {
		t.Fatalf("progress percentage = %#v, want 42", progress.Percentage)
	}
	if progress.Phase != detail.Type || progress.Message != detail.Status {
		t.Fatalf("progress summary = %#v, want typed phase and status message", progress)
	}
	if progress.Detail != detail {
		t.Fatalf("progress detail = %#v, want %#v", progress.Detail, detail)
	}
}

func TestApplyOfflineUpdatesTriggersPreparedUpdate(t *testing.T) {
	service := setupFakePackageKit(t, true)

	if _, err := applyOfflineUpdates(context.Background()); err != nil {
		t.Fatalf("applyOfflineUpdates: %v", err)
	}
	if got := service.triggers(); !slices.Equal(got, []string{"reboot"}) {
		t.Fatalf("offline triggers = %#v", got)
	}
}

func TestApplyOfflineUpdatesReturnsTriggerError(t *testing.T) {
	service := setupFakePackageKit(t, true)
	service.setTriggerErr(godbus.MakeFailedError(errors.New("boom")))

	_, err := applyOfflineUpdates(context.Background())
	if err == nil {
		t.Fatal("applyOfflineUpdates returned nil error")
	}
	if !strings.Contains(err.Error(), "failed to trigger offline update") {
		t.Fatalf("error = %q", err)
	}
}

func hasProgressType(frames []PkgUpdateProgress, typ string) bool {
	for _, frame := range frames {
		if frame.Type == typ {
			return true
		}
	}
	return false
}

func TestPackageKitSignalLoopsFailOnClosedChannel(t *testing.T) {
	newClosed := func() <-chan *dbusclient.Signal {
		ch := make(chan *dbusclient.Signal)
		close(ch)
		return ch
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, collectErr := collectUpdatePackages(ctx, newClosed())
	_, detailErr := collectSingleUpdateDetail(ctx, newClosed(), "pkg;1;amd64;repo")
	awaitErr := awaitPackageUpdateSignals(ctx, newClosed(), nil, nil)
	for name, err := range map[string]error{"collectUpdatePackages": collectErr, "collectSingleUpdateDetail": detailErr, "awaitPackageUpdateSignals": awaitErr} {
		if !errors.Is(err, pkgkit.ErrSignalsClosed) {
			t.Errorf("%s error = %v, want ErrSignalsClosed", name, err)
		}
	}
	if ctx.Err() != nil {
		t.Fatal("loops spun until the context expired instead of returning promptly")
	}
}

func TestCollectUpdatePackagesReturnsContextErrorOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, _, err := collectUpdatePackages(ctx, make(chan *dbusclient.Signal)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
}
