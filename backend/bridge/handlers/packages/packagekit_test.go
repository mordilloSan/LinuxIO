package packages

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

type fakePackage struct {
	info    uint32
	id      string
	summary string
}

type fakePackageKit struct {
	t        *testing.T
	conn     *godbus.Conn
	prepared bool

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
	refreshes       int
	offlineTriggers []string
	triggerErr      *godbus.Error
}

type fakeTransaction struct {
	service *fakePackageKit
	path    godbus.ObjectPath
}

func setupFakePackageKit(t *testing.T, prepared bool) *fakePackageKit {
	t.Helper()

	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	t.Cleanup(func() {
		_ = dbusclient.CloseSignals(context.Background())
	})

	conn := bus.OwnName(t, dbusclient.PackageKitBusName)
	service := &fakePackageKit{
		t:             t,
		conn:          conn,
		prepared:      prepared,
		details:       make(map[string][]any),
		updateErrors:  make(map[string]string),
		updateHangs:   make(map[string]bool),
		updateStarted: make(chan string, 16),
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

func (s *fakePackageKit) triggers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.offlineTriggers...)
}

func (tx *fakeTransaction) RefreshCache(force bool) *godbus.Error {
	tx.service.mu.Lock()
	if force {
		tx.service.refreshes++
	}
	tx.service.mu.Unlock()
	tx.emitLater(func() {
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
	tx.service.mu.Unlock()
	tx.emitLater(func() {
		tx.emit("Finished", uint32(0), uint32(0))
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
		tx.emit("Finished", uint32(0), uint32(0))
	})
	return nil
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
