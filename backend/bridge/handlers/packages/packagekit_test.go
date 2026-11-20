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
	// cancelled receives the transaction of each Cancel call.
	cancelled chan godbus.ObjectPath
	// refuseCancel answers Cancel with an error, as PackageKit's main branch
	// does while its backend cannot cancel.
	refuseCancel bool
	// cancelEnds ends a transaction that is asked to cancel, with the
	// cancelled exit, as apt's backend does while it downloads. Otherwise the
	// transaction runs on, as dnf5's backend, which cannot cancel, does.
	cancelEnds bool
	// cannotCancel makes every transaction's AllowCancel false, as while dpkg
	// runs.
	cannotCancel bool
	// installHangs leaves a package's install running until the test ends
	// it; installStarted receives each install's transaction.
	installHangs   map[string]bool
	installStarted chan *fakeTransaction
	// holdInstallReply, when set, keeps InstallPackages from answering
	// until it is closed, with installStarted told at once.
	holdInstallReply chan struct{}
	// installsUnlisted keeps installs out of the transaction list, as
	// PackageKit keeps a transaction that still waits for polkit.
	installsUnlisted bool
	// finishes counts the Finished signals sent.
	finishes int
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
	// asked is set once a method asked the transaction to do something,
	// under service.mu: PackageKit answers a Cancel before that with NoRole.
	asked bool
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
		cancelled:      make(chan godbus.ObjectPath, 16),
		installHangs:   make(map[string]bool),
		installStarted: make(chan *fakeTransaction, 16),
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
	allowCancel := !s.cannotCancel
	s.mu.Unlock()

	tx := &fakeTransaction{service: s, path: path}
	if err := s.conn.Export(tx, path, dbusclient.PackageKitTransactionIface); err != nil {
		return "", godbus.MakeFailedError(err)
	}
	if _, err := prop.Export(s.conn, path, prop.Map{
		dbusclient.PackageKitTransactionIface: {"AllowCancel": {Value: allowCancel, Emit: prop.EmitTrue}},
	}); err != nil {
		return "", godbus.MakeFailedError(err)
	}
	return path, nil
}

func (s *fakePackageKit) GetTransactionList() ([]godbus.ObjectPath, *godbus.Error) {
	s.mu.Lock()
	running := slices.Clone(s.running)
	s.mu.Unlock()
	select {
	case s.listed <- struct{}{}:
	default:
	}
	return running, nil
}

// dropTransaction takes path out of the list of transactions PackageKit runs
// without a Finished, as when PackageKit crashed or restarted.
func (s *fakePackageKit) dropTransaction(path godbus.ObjectPath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = slices.DeleteFunc(s.running, func(p godbus.ObjectPath) bool { return p == path })
}

func (s *fakePackageKit) finishedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finishes
}

// runTransaction is a transaction of role that PackageKit runs at path for a
// client that is gone, such as a job host that died.
func (s *fakePackageKit) runTransaction(path godbus.ObjectPath, role uint32) *fakeTransaction {
	s.t.Helper()
	tx := &fakeTransaction{service: s, path: path, asked: true}
	if err := s.conn.Export(tx, path, dbusclient.PackageKitTransactionIface); err != nil {
		s.t.Fatalf("export transaction %s: %v", path, err)
	}
	s.mu.Lock()
	allowCancel := !s.cannotCancel
	s.mu.Unlock()
	if _, err := prop.Export(s.conn, path, prop.Map{
		dbusclient.PackageKitTransactionIface: {
			"Role":        {Value: role, Emit: prop.EmitFalse},
			"AllowCancel": {Value: allowCancel, Emit: prop.EmitTrue},
		},
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
	tx.asked = true
	tx.service.installed = append(tx.service.installed, packageIDs...)
	if !tx.service.installsUnlisted {
		tx.service.running = append(tx.service.running, tx.path)
	}
	exit := tx.service.exits[packageIDs[0]]
	hangs := tx.service.installHangs[packageIDs[0]]
	held := tx.service.holdInstallReply
	tx.service.mu.Unlock()
	if held != nil {
		tx.service.installStarted <- tx
		<-held
	} else {
		// Announced once the method has answered, so a cancel the test
		// sends next lands while the install waits for the signals.
		tx.emitLater(func() { tx.service.installStarted <- tx })
	}
	if hangs {
		return nil
	}
	tx.emitLater(func() {
		tx.emit("Finished", exit, uint32(0))
	})
	return nil
}

func (tx *fakeTransaction) UpdatePackages(flags uint64, packageIDs []string) *godbus.Error {
	tx.service.mu.Lock()
	tx.asked = true
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

// Cancel answers as PackageKit does: NoRole for a transaction never asked to
// do anything, an error while refuseCancel is set, and otherwise it accepts,
// ending the transaction only when cancelEnds is set.
func (tx *fakeTransaction) Cancel() *godbus.Error {
	tx.service.mu.Lock()
	tx.service.cancels = append(tx.service.cancels, tx.path)
	asked, refuse, ends := tx.asked, tx.service.refuseCancel, tx.service.cancelEnds
	tx.service.mu.Unlock()
	tx.service.cancelled <- tx.path
	switch {
	case !asked:
		return godbus.NewError("org.freedesktop.PackageKit.Transaction.NoRole", []any{"No role"})
	case refuse:
		return godbus.NewError("org.freedesktop.PackageKit.Transaction.CannotCancel", []any{"The transaction cannot be cancelled right now"})
	case ends:
		tx.emitLater(func() { tx.emit("Finished", uint32(3), uint32(0)) }) // PK_EXIT_ENUM_CANCELLED
	}
	return nil
}

func (s *fakePackageKit) setInstallHang(packageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installHangs[packageID] = true
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

// emit sends a signal of the transaction. PackageKit drops a transaction from
// its list as it sends its Finished.
func (tx *fakeTransaction) emit(member string, values ...any) {
	if member == "Finished" {
		tx.service.mu.Lock()
		tx.service.finishes++
		tx.service.running = slices.DeleteFunc(tx.service.running, func(p godbus.ObjectPath) bool { return p == tx.path })
		tx.service.mu.Unlock()
	}
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

	if err := installPackage(context.Background(), "demo;1.2.3;x86_64;repo", nil); err != nil {
		t.Fatalf("installPackage: %v", err)
	}
	if got := service.installedPackages(); !slices.Equal(got, []string{"demo;1.2.3;x86_64;repo"}) {
		t.Fatalf("installed = %#v", got)
	}
	if got := service.canceled(); len(got) != 0 {
		t.Fatalf("PackageKit Cancel on %v, want none for an install nobody cancelled", got)
	}
}

const hungPackageID = "zram-tools;0.3.6-1;all;ubuntu"

// installOutcome is what an install returned, and whether PackageKit had sent
// a Finished by then.
type installOutcome struct {
	err      error
	finished bool
}

// startHungInstall starts an install whose PackageKit transaction runs until
// the test ends it, and returns that transaction and the install's outcome.
func startHungInstall(ctx context.Context, t *testing.T, service *fakePackageKit) (*fakeTransaction, <-chan installOutcome) {
	t.Helper()
	service.setInstallHang(hungPackageID)
	done := make(chan installOutcome, 1)
	go func() {
		err := installPackage(ctx, hungPackageID, nil)
		done <- installOutcome{err: err, finished: service.finishedCount() > 0}
	}()
	select {
	case tx := <-service.installStarted:
		return tx, done
	case out := <-done:
		t.Fatalf("the install returned %v before PackageKit started it", out.err)
	case <-time.After(5 * time.Second):
		t.Fatal("PackageKit was not asked to install")
	}
	return nil, nil
}

func waitCancelCall(t *testing.T, service *fakePackageKit) {
	t.Helper()
	select {
	case <-service.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("no Cancel reached PackageKit")
	}
}

// requireStillWaiting fails when the install returned while PackageKit's
// transaction still runs.
func requireStillWaiting(t *testing.T, done <-chan installOutcome) {
	t.Helper()
	select {
	case out := <-done:
		t.Fatalf("the install returned %v while PackageKit's transaction still ran", out.err)
	case <-time.After(200 * time.Millisecond):
	}
}

// waitCancelledInstall waits for the install to return, and fails unless it
// returned the cancel and, when wantFinished, after PackageKit's Finished.
func waitCancelledInstall(t *testing.T, done <-chan installOutcome, wantFinished bool) {
	t.Helper()
	select {
	case out := <-done:
		if !errors.Is(out.err, context.Canceled) {
			t.Fatalf("install = %v, want canceled", out.err)
		}
		if wantFinished && !out.finished {
			t.Fatal("the install returned before PackageKit's Finished")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the install did not return")
	}
}

// A cancelled install asks PackageKit once to cancel its transaction and
// returns only after the transaction's Finished, so what runs after the
// package step never runs while the package's scripts may.
func TestCancelledInstallCancelsTheTransactionAndWaitsForItsEnd(t *testing.T) {
	service := setupFakePackageKit(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelCall(t, service)
	requireStillWaiting(t, done)
	tx.emit("Finished", uint32(3), uint32(0)) // PK_EXIT_ENUM_CANCELLED

	waitCancelledInstall(t, done, true)
	if got := service.canceled(); !slices.Equal(got, []godbus.ObjectPath{tx.path}) {
		t.Fatalf("PackageKit Cancel on %v, want once on %s", got, tx.path)
	}
}

// A transaction that ends on the cancel, as apt's backend ends one while it
// downloads, ends the wait, and only its Finished does.
func TestCancelledInstallReturnsWhenPackageKitEndsTheTransaction(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.cancelEnds = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelledInstall(t, done, true)
	if got := service.canceled(); len(got) != 1 {
		t.Fatalf("PackageKit Cancel calls = %v, want one", got)
	}
}

// A cancel that lands while the InstallPackages call is still out may come
// after PackageKit took the call: the transaction is cancelled and waited for
// all the same.
func TestCancelDuringTheInstallCallCancelsAndWaits(t *testing.T) {
	service := setupFakePackageKit(t, false)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseReply := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseReply)
	service.holdInstallReply = release
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelCall(t, service)
	releaseReply()
	requireStillWaiting(t, done)
	tx.emit("Finished", uint32(3), uint32(0))

	waitCancelledInstall(t, done, true)
	if got := service.canceled(); !slices.Equal(got, []godbus.ObjectPath{tx.path}) {
		t.Fatalf("PackageKit Cancel on %v, want once on %s", got, tx.path)
	}
}

// A transaction that ends with an error after the cancel ends the wait: the
// install still returns the cancel, and the error is logged.
func TestCancelledInstallLogsTheTransactionsError(t *testing.T) {
	logs := captureLogs(t)
	service := setupFakePackageKit(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelCall(t, service)
	tx.emit("ErrorCode", uint32(1), "dpkg was interrupted")
	tx.emit("Finished", uint32(2), uint32(0)) // PK_EXIT_ENUM_FAILED

	waitCancelledInstall(t, done, true)
	if !strings.Contains(logs.String(), `level=DEBUG msg="PackageKit install transaction failed after a cancel"`) || !strings.Contains(logs.String(), "dpkg was interrupted") {
		t.Fatalf("logs = %q, want the transaction's error at Debug", logs.String())
	}
}

// PackageKit dropping the transaction without a Finished, as when it crashed
// or restarted, ends the wait: while it lists the transaction the install
// waits through every check, and once two checks in a row miss it the
// install returns, with a Warn line.
func TestCancelledInstallStopsWaitingWhenPackageKitDropsTheTransaction(t *testing.T) {
	logs := captureLogs(t)
	service := setupFakePackageKit(t, false)
	previous := transactionPollInterval
	transactionPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { transactionPollInterval = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelCall(t, service)
	requireStillWaiting(t, done)
	service.dropTransaction(tx.path)

	waitCancelledInstall(t, done, false)
	if !strings.Contains(logs.String(), `level=WARN msg="PackageKit install transaction gone without a Finished after a cancel"`) {
		t.Fatalf("logs = %q, want the transaction's loss recorded", logs.String())
	}
}

// A transaction PackageKit has not listed at any check, as one that still
// waits for polkit, is not taken for gone: the install waits for its
// Finished, and otherwise for the bound.
func TestCancelledInstallWaitsForATransactionNeverListed(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.installsUnlisted = true
	previous := transactionPollInterval
	transactionPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { transactionPollInterval = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelCall(t, service)
	requireStillWaiting(t, done)
	tx.emit("Finished", uint32(3), uint32(0))

	waitCancelledInstall(t, done, true)
}

// A transaction PackageKit does not cancel is waited for to its end all the
// same: one it says cannot be cancelled now is not asked, one whose cancel
// fails or is ignored runs on.
func TestCancelledInstallWaitsForATransactionThatRunsOn(t *testing.T) {
	tests := map[string]struct {
		setup       func(*fakePackageKit)
		wantCancels int
	}{
		// AllowCancel false, as while dpkg runs: PackageKit up to 1.4.0 would
		// cancel anyway, and its apt backend would stop apt in mid-dpkg.
		"cannot cancel now": {setup: func(s *fakePackageKit) { s.cannotCancel = true }},
		// A D-Bus error, as PackageKit's CannotCancel.
		"cancel refused": {setup: func(s *fakePackageKit) { s.refuseCancel = true }, wantCancels: 1},
		// Accepted and ignored, as by dnf5's backend, which cannot cancel.
		"cancel ignored": {setup: func(*fakePackageKit) {}, wantCancels: 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			service := setupFakePackageKit(t, false)
			tc.setup(service)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx, done := startHungInstall(ctx, t, service)

			cancel()
			if tc.wantCancels > 0 {
				waitCancelCall(t, service)
			}
			requireStillWaiting(t, done)
			tx.emit("Finished", uint32(1), uint32(0)) // PK_EXIT_ENUM_SUCCESS

			waitCancelledInstall(t, done, true)
			if got := service.canceled(); len(got) != tc.wantCancels {
				t.Fatalf("PackageKit Cancel calls = %v, want %d", got, tc.wantCancels)
			}
		})
	}
}

// The wait after a cancel keeps the install's own bound: a transaction that
// never ends is no longer waited for once it runs out, and that is logged.
func TestCancelledInstallStopsWaitingAtTheBound(t *testing.T) {
	logs := captureLogs(t)
	service := setupFakePackageKit(t, false)
	previous := installTransactionTimeout
	installTransactionTimeout = 2 * time.Second
	t.Cleanup(func() { installTransactionTimeout = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	_, done := startHungInstall(ctx, t, service)

	cancel()
	waitCancelledInstall(t, done, false)
	if elapsed := time.Since(start); elapsed < installTransactionTimeout {
		t.Fatalf("the install returned after %s, before its bound of %s", elapsed, installTransactionTimeout)
	}
	if got := service.canceled(); len(got) != 1 {
		t.Fatalf("PackageKit Cancel calls = %v, want one", got)
	}
	if !strings.Contains(logs.String(), `level=WARN msg="PackageKit install transaction outlived the wait after a cancel"`) {
		t.Fatalf("logs = %q, want the transaction left running recorded", logs.String())
	}
}

// A transaction PackageKit was never asked to run, as when the cancel lands
// before InstallPackages is sent, answers the Cancel with NoRole and sends no
// Finished: it is not waited for.
func TestCancelledInstallDoesNotWaitForATransactionNeverStarted(t *testing.T) {
	service := setupFakePackageKit(t, false)
	done := make(chan error, 1)
	go func() {
		done <- pkgkit.Run(context.Background(), pkgkit.OperationOptions{NoRetry: true}, func(session pkgkit.ClientSession) error {
			trans, err := session.CreateTransaction(20)
			if err != nil {
				return err
			}
			defer pkgkit.LogClose(session.Context(), trans)
			awaitCancelledInstall(session.Context(), trans, time.Now().Add(time.Minute), nil, ignoreProgress)
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PackageKit session: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled install waited for a transaction that never ran")
	}
	if got := service.canceled(); len(got) != 1 {
		t.Fatalf("PackageKit Cancel calls = %v, want one", got)
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

			if err := InstallByName(context.Background(), "zram-generator", report); err != nil {
				t.Fatalf("InstallByName: %v", err)
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

			if err := InstallByName(context.Background(), "zram-generator", func(*PkgUpdateProgress) error { return nil }); err != nil {
				t.Fatalf("InstallByName: %v", err)
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

	err := InstallByName(context.Background(), "no-such-package", nil)
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

	err := InstallByName(context.Background(), "zram-generator", func(*PkgUpdateProgress) error { return nil })
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
		errCh <- InstallByName(ctx, "zram-generator", func(*PkgUpdateProgress) error { return nil })
	}()

	select {
	case <-service.refreshStarted:
	case err := <-errCh:
		t.Fatalf("InstallByName returned %v before refreshing", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the cache refresh to start")
	}
	cancel()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("InstallByName did not return after cancellation")
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

	if err := updatePackages(context.Background(), []string{"demo;1.2.3;x86_64;repo"}, report); err != nil {
		t.Fatalf("updatePackages: %v", err)
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

	err := updatePackages(context.Background(), packageIDs, report)
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

	err := updatePackages(
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
		errCh <- updatePackages(ctx, packageIDs, func(*PkgUpdateProgress) error { return nil })
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
