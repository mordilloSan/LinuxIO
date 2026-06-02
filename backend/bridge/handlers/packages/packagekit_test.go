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
		t:        t,
		conn:     conn,
		prepared: prepared,
		details:  make(map[string][]any),
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

func (s *fakePackageKit) triggers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.offlineTriggers...)
}

func (tx *fakeTransaction) RefreshCache(force bool) *godbus.Error {
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
	tx.service.mu.Unlock()
	tx.emitLater(func() {
		tx.emit("Package", uint32(11), packageIDs[0], "Kernel update")
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
		tx.emit("ItemProgress", packageIDs[0], uint32(10), uint32(50))
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

func TestInstallPackageCallsInstallPackagesAndWaits(t *testing.T) {
	service := setupFakePackageKit(t, false)

	if err := InstallPackage(context.Background(), "demo;1.2.3;x86_64;repo"); err != nil {
		t.Fatalf("InstallPackage: %v", err)
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
