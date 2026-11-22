package memory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// The root tests run only in the disposable test VM, through
// `make vm-test-memory-integration`; the helpers below serve them.

// needRoot skips a root test unless the rig asked for it and it runs as root.
func needRoot(t *testing.T) {
	t.Helper()
	if os.Getenv("LINUXIO_RUN_MEMORY_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_MEMORY_INTEGRATION=1 and run as root in the test VM")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
}

// logProgress logs each step an operation reports.
func logProgress(t *testing.T) func(apischema.SwapProgress) {
	return func(p apischema.SwapProgress) { t.Logf("step %d/%d %s %s", p.Index, p.Total, p.Step, p.Device) }
}

// stepLog logs each step an operation reports and keeps their codes.
type stepLog struct {
	t     *testing.T
	steps []apischema.SwapStep
}

func (l *stepLog) report(p apischema.SwapProgress) {
	logProgress(l.t)(p)
	l.steps = append(l.steps, p.Step)
}

// itExecutor is the executor the routes use, logging its steps.
func itExecutor(t *testing.T) *executor {
	return newExecutor(logProgress(t))
}

// freshStatus reads the host's status as the routes do.
func freshStatus(t *testing.T) apischema.SwapStatus {
	t.Helper()
	status, err := GetSwapStatus(context.WithoutCancel(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	return status
}

// mustRow is the row of path in a fresh status.
func mustRow(t *testing.T, path string) apischema.SwapFileRow {
	t.Helper()
	status := freshStatus(t)
	row, ok := findRow(status, path)
	if !ok {
		t.Fatalf("no row for %s in %+v", path, status.SwapFiles)
	}
	return row
}

// resetLeftZram resets a zram0 that a stop left set up, as a change does
// before it starts zram (resetStaleZram): the tests start zram themselves.
func resetLeftZram(ctx context.Context, e *executor, provider apischema.ZramProvider) error {
	run := &modeRun{executor: e, before: apischema.SwapStatus{ZramProvider: provider}}
	return run.resetStaleZram(ctx)
}

// zramIs returns a check that a zram device swaps, or does not.
func (e *executor) zramIs(on bool) func() (bool, error) {
	return func() (bool, error) {
		devices, err := e.root.readSwaps()
		return err == nil && hasDevice(devices, apischema.SwapDeviceZram) == on, err
	}
}

// hostZramRequest is a switch to zram that confirms removing exactly the swap
// files a fresh status says it removes, and replacing confirmReplace.
func hostZramRequest(t *testing.T, confirmReplace string) apischema.SwapModeRequest {
	t.Helper()
	return apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: confirmReplace, ConfirmRemove: zramRemovals(freshStatus(t).SwapFiles)}
}

// switchTo switches the host to mode through the route's function,
// confirming the removal of every swap file a switch to zram removes, and
// fails the test unless the host ends in that mode with nothing left as it
// was. A switch to zram also checks that each file it removed is gone.
func switchTo(t *testing.T, mode apischema.SwapMode) apischema.SwapStatus {
	t.Helper()
	clearZramStartLimit(t)
	req := apischema.SwapModeRequest{Mode: mode}
	if mode == apischema.SwapModeZram {
		req = hostZramRequest(t, "")
	}
	got, err := SetSwapMode(t.Context(), req, logProgress(t))
	if err != nil {
		t.Fatalf("switch to %s: %v", mode, err)
	}
	if got.Mode != mode || len(got.Left) > 0 {
		t.Fatalf("after switching to %s: mode %s, left as it was %+v", mode, got.Mode, got.Left)
	}
	for _, path := range req.ConfirmRemove {
		assertSwapFileGone(t, path)
	}
	return got.SwapStatus
}

// clearZramStartLimit clears the failed state and the start counter of the
// zram providers' units. The tests start and stop zram several times within
// systemd's start limit of five starts in ten seconds, which a person at the
// dialog does not, and zramswap.service fails each stop that cannot unload
// the zram module; systemd would then refuse the next start as "repeated too
// quickly".
func clearZramStartLimit(t *testing.T) {
	t.Helper()
	for _, unit := range []string{zramService, zramGeneratorSwapUnit, zramGeneratorSetupUnit} {
		_ = systemd.ResetFailedUnit(context.WithoutCancel(t.Context()), unit)
	}
}

// asJobHost gives the test the umask the job host, which runs the changes,
// runs with, until it ends.
func asJobHost(t *testing.T) {
	previous := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(previous) })
}

// swapUnit is the unit systemd-fstab-generator makes for the swap file at
// path.
func swapUnit(t *testing.T, path string) string {
	t.Helper()
	name, err := systemd.EscapePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return name + ".swap"
}

// assertSwapFileGone checks that a removed swap file is gone everywhere: not
// in /proc/swaps, no /etc/fstab line, not on disk, and its unit not active.
func assertSwapFileGone(t *testing.T, path string) {
	t.Helper()
	if listed, err := swapListed(host, path); err != nil || listed {
		t.Errorf("%s in /proc/swaps = %v, %v; want it gone", path, listed, err)
	}
	if lines := fstabSwapLines(t, path); len(lines) != 0 {
		t.Errorf("/etc/fstab still has %q", lines)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still on disk: %v", path, err)
	}
	unit := swapUnit(t, path)
	if state, err := systemd.GetActiveState(t.Context(), unit); err == nil && state == "active" {
		t.Errorf("%s = %q; want it not active", unit, state)
	}
}

// assertSwapsAt checks that path swaps at size bytes less the header's page
// and that its unit, which systemd makes from its fstab line, is active:
// systemd follows LinuxIO's own swapon through /proc/swaps.
func assertSwapsAt(t *testing.T, path string, size uint64) {
	t.Helper()
	dev := waitSwapListed(t, host, path, true)
	if want := size - uint64(os.Getpagesize()); dev.Size != want {
		t.Errorf("%s swaps %d MiB, want %d MiB", path, dev.Size>>20, size>>20)
	}
	unit := swapUnit(t, path)
	if err := waitUntil(t.Context(), 15*time.Second, 100*time.Millisecond, func() (bool, error) {
		state, err := systemd.GetActiveState(t.Context(), unit)
		return state == "active", err
	}); err != nil {
		t.Errorf("%s not active: %v", unit, err)
	}
}

// stateDirFor makes LinuxIO's state folder where LinuxIO is not installed, as
// its tmpfiles.d entry makes it, and has cleanup remove LinuxIO's swap folder
// when the test leaves it empty and the state folder when the test made it.
// It is called before anything that registers a cleanup which removes
// LinuxIO's swap file, so that its own cleanup runs after those.
func stateDirFor(t *testing.T) {
	t.Helper()
	made := makeStateDir(t)
	t.Cleanup(func() { removeOwnSwapDir(t, made) })
}

// makeStateDir makes /var/lib/linuxio, 0755, when it is missing, and reports
// whether it did.
func makeStateDir(t *testing.T) bool {
	t.Helper()
	stateDir := filepath.Dir(ownSwapDir)
	if _, err := os.Stat(stateDir); !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err := os.Mkdir(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return true
}

// removeOwnSwapDir removes LinuxIO's swap folder, an empty subvolume on
// btrfs, and the state folder when stateMade says a test made it and
// nothing else is in it.
func removeOwnSwapDir(t *testing.T, stateMade bool) {
	t.Helper()
	if err := rmdir(ownSwapDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("remove %s: %v", ownSwapDir, err)
	}
	if stateMade {
		if err := rmdir(filepath.Dir(ownSwapDir)); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			t.Errorf("remove %s: %v", filepath.Dir(ownSwapDir), err)
		}
	}
}

// rmdir removes an empty folder or an empty subvolume, which rmdir(2)
// removes like a folder.
func rmdir(path string) error {
	if err := syscall.Rmdir(path); err != nil {
		return &os.PathError{Op: "rmdir", Path: path, Err: err}
	}
	return nil
}

// swapBaseline is the swap files a test found, other than LinuxIO's own,
// with their fstab lines and lengths, so that its cleanup can make again
// what the test removed and give back what it resized.
type swapBaseline struct {
	files     []baselineFile
	ownExists bool
}

type baselineFile struct {
	path   string
	length uint64
	active bool
	lines  []string
}

func takeSwapBaseline(t *testing.T) swapBaseline {
	t.Helper()
	status := freshStatus(t)
	var b swapBaseline
	for _, row := range status.SwapFiles {
		if row.Path == ownSwapFilePath {
			b.ownExists = true
			continue
		}
		if row.Temporary || !row.InFstab {
			continue
		}
		info, err := os.Lstat(row.Path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		b.files = append(b.files, baselineFile{path: row.Path, length: uint64(info.Size()), active: row.Active, lines: fstabSwapLines(t, row.Path)})
	}
	return b
}

// restoreFiles makes again each baseline file that is gone, with its fstab
// lines as they were, turns on the ones that swapped, and gives back the
// length of one a test resized. A temporary file a test left is removed.
func (b swapBaseline) restoreFiles(t *testing.T) {
	t.Helper()
	for _, f := range b.files {
		if f.restoreOn(t) {
			f.restoreSize(t)
		}
	}
}

// restoreOn makes the file again when it is gone, with its fstab lines, and
// turns it on when it swapped; it reports whether that went well.
func (f baselineFile) restoreOn(t *testing.T) bool {
	t.Helper()
	ctx := context.WithoutCancel(t.Context())
	if _, err := os.Lstat(f.path); errors.Is(err, fs.ErrNotExist) {
		if _, err := host.makeSwapFile(ctx, f.path, f.length); err != nil {
			t.Errorf("make %s again: %v", f.path, err)
			return false
		}
	}
	if len(fstabSwapLines(t, f.path)) == 0 {
		for _, line := range f.lines {
			appendFstab(t, line)
		}
		if err := systemd.DaemonReload(ctx); err != nil {
			t.Errorf("reload systemd: %v", err)
		}
	}
	if listed, _ := swapListed(host, f.path); !f.active || listed {
		return true
	}
	options := ""
	if fields := strings.Fields(f.lines[0]); len(fields) > 3 {
		options = fields[3]
	}
	if err := host.swapOn(ctx, f.path, options); err != nil {
		t.Errorf("turn %s on again: %v", f.path, err)
		return false
	}
	waitSwapListed(t, host, f.path, true)
	return true
}

// restoreSize removes a temporary file a test left beside the file, and
// gives the file back the length it had.
func (f baselineFile) restoreSize(t *testing.T) {
	t.Helper()
	ctx := context.WithoutCancel(t.Context())
	temporary := f.path + resizeSuffix
	if _, err := os.Lstat(temporary); err == nil {
		if _, err := RemoveSwapFile(ctx, apischema.SwapFilePathRequest{Path: temporary}, logProgress(t)); err != nil {
			_ = os.Remove(temporary)
		}
	}
	info, err := os.Lstat(f.path)
	if err != nil || uint64(info.Size()) == f.length || !f.active {
		return
	}
	if _, err := ResizeSwapFile(ctx, apischema.SwapFileResizeRequest{Path: f.path, Size: f.length}, logProgress(t)); err != nil {
		t.Errorf("give %s back its %d MiB: %v", f.path, f.length>>20, err)
	}
}

// removeOwn removes LinuxIO's swap file when the test made it.
func (b swapBaseline) removeOwn(t *testing.T) {
	t.Helper()
	if b.ownExists {
		return
	}
	if _, err := os.Lstat(ownSwapFilePath); err != nil {
		return
	}
	if _, err := RemoveSwapFile(context.WithoutCancel(t.Context()), apischema.SwapFilePathRequest{Path: ownSwapFilePath}, logProgress(t)); err != nil {
		t.Errorf("remove %s, which the test made: %v", ownSwapFilePath, err)
	}
}

// keepSwapFiles has cleanup put the swap files back as the test found them:
// what it removed made again first, then LinuxIO's file removed when the
// test made it, so the host gains swap before it loses any.
func keepSwapFiles(t *testing.T) {
	t.Helper()
	stateDirFor(t)
	b := takeSwapBaseline(t)
	t.Cleanup(func() {
		b.restoreFiles(t)
		b.removeOwn(t)
	})
}

// TestMemoryIntegration switches the real host to zram and back through
// systemd, the kernel and /etc. A switch to zram removes every swap file
// LinuxIO can remove, after a confirm that names them: one that names
// anything else is refused and changes nothing. The way back to disk makes
// LinuxIO's swap file, since none is left. It switches to zram once more
// from zram running at another size beside the swap file, and from a zram0
// a stop left set up. It then asks before replacing an admin's zram
// configuration and keeps its copy. With zram-tools it has systemd give up a
// stop of zramswap.service. With zram-generator it refuses to leave zram
// while another of its files defines a swap device, and checks that the
// generator makes a unit for each device the status lists. Last it switches
// to zswap and back with its tmpfiles entry, and has the kernel refuse a
// compressor it does not have. It needs root, a zram provider and a swap
// file LinuxIO can remove, so it runs only in the disposable test VM:
// `make vm-test-memory-integration`. A host that starts mixed, as a fresh
// zram-generator install leaves it, goes to disk first. Cleanup puts the
// host back as it was: its swap files, its mode, the files LinuxIO manages
// and the copies it made, zram-tools' start at boot, and the live sysctls
// and zswap.
func TestMemoryIntegration(t *testing.T) {
	needRoot(t)
	ctx := t.Context()
	provider := host.zramProvider(ctx, systemd.GetActiveState)
	if provider == "" {
		t.Skip("neither zram-tools nor zram-generator is installed")
	}
	asJobHost(t)
	e := itExecutor(t)
	// A run that was cut short can have left files of its own behind.
	removeTestUnitFiles(t, e, nil)
	view, err := e.readView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(zramRemovals(view.status.SwapFiles)) == 0 {
		t.Skip("no swap file LinuxIO can remove: the rig adds /swap.img")
	}
	start := view.status.Mode
	t.Logf("zram provider %s, mode %s, configuration not LinuxIO's %+v, other generator devices %+v, main generator files %v",
		provider, start, view.status.ForeignZramConfig, view.status.ZramGeneratorDevices, existingGeneratorMainFiles())
	if start == apischema.SwapModeNone {
		t.Skip("start from disk, zswap, zram or mixed, not none")
	}
	snapshot := takeHostSnapshot(t, view.status)
	t.Cleanup(func() { snapshot.restore(t, e, provider) })
	// Runs before the restore: what systemd and the kernel say about zram
	// when a step failed.
	t.Cleanup(func() {
		if t.Failed() {
			logZramState(t)
		}
	})
	back := start
	if start == apischema.SwapModeZram || start == apischema.SwapModeMixed {
		back = apischema.SwapModeDisk
	}
	if start == apischema.SwapModeMixed {
		switchTo(t, back)
	}

	assertZramWithoutTheConfirmChangesNothing(t)
	if start != apischema.SwapModeZram {
		switchTo(t, apischema.SwapModeZram)
	}
	assertZramMode(t, e)

	assertDiskMode(t, switchTo(t, back))

	// zram that swaps at another size beside the swap file, as the one
	// zram-tools starts when it is installed, ends at the planned size.
	startSmallZram(t, e)
	switchTo(t, apischema.SwapModeZram)
	assertZramMode(t, e)

	assertDiskMode(t, switchTo(t, back))

	assertStartsZramAStopLeftSetUp(t, e, provider, back)

	assertAsksBeforeReplacingAnAdminsFile(t, e, provider, back)
	if provider == apischema.ZramProviderTools {
		assertGivesUpAStopSystemdTimedOut(t, e)
	}
	if provider == apischema.ZramProviderGenerator {
		assertListsADropInDeviceAndRefusesToLeaveZram(t, e)
		assertGeneratorUnitsMatchTheStatus(t, e)
	}
	assertZswapRoundTrip(t, e)
}

// assertZramWithoutTheConfirmChangesNothing asks for zram with no confirm,
// and with one that names a file the switch does not remove: both are
// refused, naming the files it removes, and nothing changes.
func assertZramWithoutTheConfirmChangesNothing(t *testing.T) {
	t.Helper()
	removed := zramRemovals(freshStatus(t).SwapFiles)
	fstab, swaps := readFile(t, etcFstab), readFile(t, procSwaps)
	managed := map[string]string{}
	for _, name := range []string{sysctlDropInPath, zswapTmpfilesPath, zramswapConfigPath, zramGeneratorConfigPath} {
		managed[name], _, _ = host.readOptional(name)
	}
	for _, confirm := range [][]string{nil, {"/linuxio-it-not-a-swap-file"}, removed[1:]} {
		_, err := SetSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmRemove: confirm}, logProgress(t))
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), removed[0]) {
			t.Fatalf("switch to zram confirming %q: %v, want a conflict naming %v", confirm, err, removed)
		}
	}
	if got := readFile(t, etcFstab); got != fstab {
		t.Errorf("/etc/fstab = %q after the refusals, want %q", got, fstab)
	}
	if got := readFile(t, procSwaps); got != swaps {
		t.Errorf("/proc/swaps = %q after the refusals, want %q", got, swaps)
	}
	for _, path := range removed {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("%s after the refusals: %v", path, err)
		}
	}
	for name, before := range managed {
		if after, _, _ := host.readOptional(name); after != before {
			t.Errorf("%s = %q after the refusals, want %q", name, after, before)
		}
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// logZramState logs what systemd and the kernel say about zram0 and its
// units, for a failed run.
func logZramState(t *testing.T) {
	t.Helper()
	for _, args := range [][]string{
		{"systemctl", "--no-pager", "status", zramGeneratorSwapUnit, zramGeneratorSetupUnit, "dev-zram0.device", zramService},
		{"journalctl", "--no-pager", "-n", "80", "-u", zramGeneratorSwapUnit, "-u", zramGeneratorSetupUnit, "-u", zramService},
		{"systemctl", "--no-pager", "cat", zramGeneratorSwapUnit, zramGeneratorSetupUnit},
		{"ls", "-l", "/dev/zram0", "/sys/class/zram-control"},
		{"cat", "/proc/swaps", "/sys/block/zram0/disksize", "/sys/block/zram0/comp_algorithm", "/etc/fstab"},
	} {
		out, err := exec.CommandContext(context.WithoutCancel(t.Context()), args[0], args[1:]...).CombinedOutput()
		t.Logf("$ %s (%v)\n%s", strings.Join(args, " "), err, out)
	}
}

// testDropIn is the zram-generator drop-in the test writes, and removes.
const testDropIn = zramGeneratorEtcDropIns + "/90-linuxio-integration-test.conf"

// existingGeneratorMainFiles names the zram-generator main files on the host,
// for the log: the first of them in /run, /etc, /usr/local/lib, /usr/lib is
// the one the generator reads.
func existingGeneratorMainFiles() []string {
	var found []string
	for _, dir := range zramGeneratorDirs {
		if name := dir + "/zram-generator.conf"; host.exists(name) {
			found = append(found, name)
		}
	}
	return found
}

// hostSnapshot is the host as a test found it.
type hostSnapshot struct {
	status apischema.SwapStatus
	// files are the files LinuxIO manages and the copies it may make.
	files []savedFile
	// toolsState is zramswap.service's file state, or empty without
	// zram-tools.
	toolsState    string
	dropInDirSeen bool
	swaps         swapBaseline
}

// takeHostSnapshot saves the files LinuxIO manages and the swap files, and
// makes LinuxIO's state folder where LinuxIO is not installed, which a
// switch to disk or zswap makes its swap file in.
func takeHostSnapshot(t *testing.T, status apischema.SwapStatus) hostSnapshot {
	t.Helper()
	stateDirFor(t)
	snapshot := hostSnapshot{status: status, dropInDirSeen: host.exists(zramGeneratorEtcDropIns), swaps: takeSwapBaseline(t)}
	names := []string{sysctlDropInPath, zswapTmpfilesPath, zramswapConfigPath, zramGeneratorConfigPath,
		zramswapConfigPath + zramConfigCopySuffix, zramGeneratorConfigPath + zramConfigCopySuffix}
	for _, name := range names {
		saved, err := host.saveFile(name)
		if err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		snapshot.files = append(snapshot.files, saved)
	}
	if tools, _ := host.zramToolsInstalled(); tools {
		snapshot.toolsState = unitFileState(t, zramService)
	}
	return snapshot
}

// restore puts the host back as the snapshot found it: the swap files a test
// removed made again before the switch back to the mode it started in, then
// LinuxIO's file removed when the test made it. zram that swapped beside the
// swap file, which no mode reaches, is started again through its provider's
// unit once the files are back.
func (s hostSnapshot) restore(t *testing.T, e *executor, provider apischema.ZramProvider) {
	ctx := context.WithoutCancel(t.Context())
	for _, name := range []string{zramswapConfigPath + zramConfigCopySuffix, zramGeneratorConfigPath + zramConfigCopySuffix} {
		if info, err := os.Lstat(name); err == nil && info.IsDir() {
			_ = os.Remove(name)
		}
	}
	if err := os.Remove(testDropIn); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("remove %s: %v", testDropIn, err)
	}
	if !s.dropInDirSeen {
		_ = os.Remove(zramGeneratorEtcDropIns)
	}
	mode := s.status.Mode
	if mode == apischema.SwapModeMixed {
		mode = apischema.SwapModeDisk
	}
	if mode != apischema.SwapModeZram {
		s.swaps.restoreFiles(t)
	}
	// After a failure the admin's test file can still be in place: the
	// files are put back after the switch anyway.
	req := apischema.SwapModeRequest{Mode: mode, ConfirmReplace: zramConfigPath(provider)}
	if mode == apischema.SwapModeZram {
		req.ConfirmRemove = zramRemovals(freshStatus(t).SwapFiles)
	}
	if _, err := SetSwapMode(ctx, req, logProgress(t)); err != nil {
		t.Errorf("restore %s: %v", mode, err)
	}
	s.swaps.removeOwn(t)
	for _, f := range s.files {
		if err := f.restore(e.sys); err != nil {
			t.Errorf("restore: %v", err)
		}
	}
	if err := e.sys.reload(ctx); err != nil {
		t.Errorf("reload: %v", err)
	}
	s.restoreUnitsAndKernel(t, e, provider)
}

// restoreUnitsAndKernel puts back zram-tools' start at boot, zram beside the
// swap file for a host that started mixed, and the live sysctls and zswap.
func (s hostSnapshot) restoreUnitsAndKernel(t *testing.T, e *executor, provider apischema.ZramProvider) {
	ctx := context.WithoutCancel(t.Context())
	switch s.toolsState {
	case "":
	case "enabled":
		if err := e.sys.enableUnit(ctx, zramService); err != nil {
			t.Errorf("enable %s: %v", zramService, err)
		}
	default:
		if err := e.sys.disableUnit(ctx, zramService); err != nil {
			t.Errorf("disable %s: %v", zramService, err)
		}
	}
	if s.status.Mode == apischema.SwapModeMixed {
		unit := zramService
		if provider == apischema.ZramProviderGenerator {
			unit = zramGeneratorSwapUnit
		}
		if err := resetLeftZram(ctx, e, provider); err != nil {
			t.Errorf("start zram beside the swap file again: %v", err)
		} else if err := e.sys.startUnit(ctx, unit); err != nil {
			t.Errorf("start zram beside the swap file again: %v", err)
		} else if err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, e.zramIs(true)); err != nil {
			t.Errorf("wait for zram beside the swap file: %v", err)
		}
	}
	params := map[string]int{sysctlSwappiness: s.status.Swappiness, sysctlPageCluster: s.status.PageCluster,
		sysctlWatermarkBoostFactor: s.status.WatermarkBoostFactor, sysctlWatermarkScaleFactor: s.status.WatermarkScaleFactor}
	for name, value := range params {
		if err := e.sys.writeParam(vmSysctlDir+"/"+name, strconv.Itoa(value)); err != nil {
			t.Errorf("restore vm.%s: %v", name, err)
		}
	}
	if !s.status.Zswap.Available {
		return
	}
	if err := e.sys.writeParam(zswapCompressorParam, s.status.Zswap.Compressor); err != nil {
		t.Errorf("restore the zswap compressor: %v", err)
	}
	if err := e.sys.writeParam(zswapEnabledParam, zswapSwitch(s.status.Zswap.Enabled)); err != nil {
		t.Errorf("restore zswap: %v", err)
	}
}

// assertAsksBeforeReplacingAnAdminsFile writes an admin's configuration for
// the provider, from a mode without zram, and checks that a switch to zram
// is refused without the confirmation, that a folder at the copy's name
// stops a confirmed one before the file changes, and that a confirmed
// switch keeps a copy with the file's mode before it replaces the file.
func assertAsksBeforeReplacingAnAdminsFile(t *testing.T, e *executor, provider apischema.ZramProvider, back apischema.SwapMode) {
	t.Helper()
	path := zramConfigPath(provider)
	admins := "# The admin's own\nSIZE=256\nPRIORITY=10\n"
	if provider == apischema.ZramProviderGenerator {
		admins = "# The admin's own\n[zram0]\nzram-size = 256\n"
	}
	copyPath := path + zramConfigCopySuffix
	writeAdminsConfig(t, path, copyPath, admins)

	_, err := SetSwapMode(t.Context(), hostZramRequest(t, ""), logProgress(t))
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), path) {
		t.Fatalf("switch to zram over an admin's %s: %v, want a conflict naming it", path, err)
	}
	assertCopyInTheWayStopsTheSwitch(t, path, copyPath, admins)

	clearZramStartLimit(t)
	got, err := SetSwapMode(t.Context(), hostZramRequest(t, path), logProgress(t))
	if err != nil || got.Mode != apischema.SwapModeZram {
		t.Fatalf("confirmed switch to zram: mode %s, %v", got.Mode, err)
	}
	saved, err := host.saveFile(copyPath)
	if err != nil || !saved.existed || string(saved.data) != admins || saved.mode != 0o640 {
		t.Fatalf("copy %s = %q mode %v, %v; want the admin's file with mode 0640", copyPath, saved.data, saved.mode, err)
	}
	if config, readErr := os.ReadFile(path); readErr != nil || !strings.HasPrefix(string(config), managedHeader) {
		t.Fatalf("%s = %q, %v; want LinuxIO's", path, config, readErr)
	}
	assertZramMode(t, e)
	assertDiskMode(t, switchTo(t, back))
}

// writeAdminsConfig puts an admin's file, mode 0640, at path, with no copy
// beside it.
func writeAdminsConfig(t *testing.T, path, copyPath, admins string) {
	t.Helper()
	for _, name := range []string{path, copyPath} {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(admins), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
}

// assertCopyInTheWayStopsTheSwitch puts a folder at the copy's name and
// checks that a confirmed switch to zram fails, naming it, with the admin's
// file as it was and the swap files still there.
func assertCopyInTheWayStopsTheSwitch(t *testing.T, path, copyPath, admins string) {
	t.Helper()
	if err := os.Mkdir(copyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	req := hostZramRequest(t, path)
	_, err := SetSwapMode(t.Context(), req, logProgress(t))
	if err == nil || !strings.Contains(err.Error(), copyPath) {
		t.Fatalf("switch to zram with a folder at %s: %v, want it named", copyPath, err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != admins {
		t.Fatalf("%s = %q, %v; want the admin's file left as it was", path, got, readErr)
	}
	for _, file := range req.ConfirmRemove {
		if listed, _ := swapListed(host, file); !listed {
			t.Errorf("%s does not swap after the switch failed at write_config, want it kept", file)
		}
	}
	if err := os.Remove(copyPath); err != nil {
		t.Fatal(err)
	}
}

// assertListsADropInDeviceAndRefusesToLeaveZram writes a drop-in that
// defines a zram swap device and checks that the status lists it and that a
// switch away from zram is refused, naming it, before anything changes; it
// is removed before any reload makes a unit from it.
func assertListsADropInDeviceAndRefusesToLeaveZram(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	if err := os.MkdirAll(zramGeneratorEtcDropIns, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testDropIn, []byte("[zram1]\nzram-size = 64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(testDropIn) }()
	status, _, err := e.sys.status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := apischema.ZramGeneratorDevice{Path: testDropIn, Device: "zram1", ZramSize: "64"}
	if !slices.Contains(status.ZramGeneratorDevices, want) {
		t.Errorf("generator devices = %+v, want %+v listed", status.ZramGeneratorDevices, want)
	}
	_, err = SetSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}, logProgress(t))
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), testDropIn) {
		t.Errorf("switch to disk with %s: %v, want a conflict naming it", testDropIn, err)
	}
}

// testGeneratorFiles are the zram-generator drop-ins
// assertGeneratorUnitsMatchTheStatus writes, in the four folders the
// generator reads: a package's in /usr/lib, one there that testGeneratorHider
// hides, one in /usr/local/lib, a zram mount in /run, one in /etc, and two of
// the same name, of which the generator reads the one in /run.
var testGeneratorFiles = []struct{ path, text string }{
	{"/usr/lib/systemd/zram-generator.conf.d/90-linuxio-integration-test-a.conf", "[zram2]\nzram-size = 64\n"},
	{"/usr/lib/systemd/zram-generator.conf.d/91-linuxio-integration-test-hidden.conf", "[zram3]\nzram-size = 64\n"},
	{"/usr/local/lib/systemd/zram-generator.conf.d/92-linuxio-integration-test-b.conf", "[zram4]\nzram-size = 64\n"},
	{"/run/systemd/zram-generator.conf.d/93-linuxio-integration-test-mount.conf", "[zram5]\nzram-size = 64\nmount-point = /run/linuxio-integration-test-zram\n"},
	{"/etc/systemd/zram-generator.conf.d/94-linuxio-integration-test-c.conf", "[zram6]\nzram-size = 64\n"},
	{"/usr/lib/systemd/zram-generator.conf.d/95-linuxio-integration-test-same.conf", "[zram7]\nzram-size = 64\n"},
	{"/run/systemd/zram-generator.conf.d/95-linuxio-integration-test-same.conf", "[zram8]\nzram-size = 64\n"},
}

// testGeneratorHider is a link to /dev/null that hides the package's drop-in
// of its name.
const testGeneratorHider = zramGeneratorEtcDropIns + "/91-linuxio-integration-test-hidden.conf"

// zramswapHangDropIn makes the stop of zramswap.service hang past a short
// timeout; in /run, so that a reboot removes it too.
const zramswapHangDropIn = "/run/systemd/system/" + zramService + ".d/90-linuxio-integration-test.conf"

// removeTestUnitFiles removes the unit and zram-generator files the tests
// write, those a run cut short left too, then the folders in made, deepest
// first, and reloads systemd when it removed anything, so no unit outlives
// its file.
func removeTestUnitFiles(t *testing.T, e *executor, made []string) {
	t.Helper()
	names := []string{testDropIn, testGeneratorHider, zramswapHangDropIn}
	for _, f := range testGeneratorFiles {
		names = append(names, f.path)
	}
	removed := false
	for _, name := range append(names, made...) {
		err := os.Remove(name)
		switch {
		case err == nil:
			removed = true
		case !errors.Is(err, fs.ErrNotExist):
			t.Errorf("remove %s: %v", name, err)
		}
	}
	if !removed {
		return
	}
	if err := e.sys.reload(context.WithoutCancel(t.Context())); err != nil {
		t.Errorf("reload: %v", err)
	}
}

// assertGeneratorUnitsMatchTheStatus writes drop-ins in the four folders
// zram-generator reads (testGeneratorFiles), and checks after a reload that
// the generator made a swap unit for exactly the devices the status lists,
// with those of the main file it reads, and a setup unit for each of them and
// for the zram mount. A reload makes units and starts none. The files go, and
// systemd is reloaded again, at the end of the step, or of the test when the
// step stops early.
func assertGeneratorUnitsMatchTheStatus(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	// The folders the files need that are not there, deepest first.
	var made []string
	for _, dir := range zramGeneratorDirs {
		for dir = filepath.Join(dir, zramGeneratorDropInDir); !host.exists(dir); dir = filepath.Dir(dir) {
			made = append(made, dir)
		}
	}
	// The generator adds a zram device for each one it makes units for, and
	// a device stays when its files go.
	devices := zramDeviceNames(t)
	undo := func() {
		removeTestUnitFiles(t, e, made)
		removeAddedZramDevices(t, devices)
	}
	t.Cleanup(undo)
	for _, f := range testGeneratorFiles {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.path, []byte(f.text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(os.DevNull, testGeneratorHider); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.reload(ctx); err != nil {
		t.Fatal(err)
	}
	status, _, err := e.sys.status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f := testGeneratorFiles
	want := []apischema.ZramGeneratorDevice{
		{Path: f[0].path, Device: "zram2", ZramSize: "64"},
		{Path: f[2].path, Device: "zram4", ZramSize: "64"},
		{Path: f[4].path, Device: "zram6", ZramSize: "64"},
		{Path: f[6].path, Device: "zram8", ZramSize: "64"},
	}
	if !slices.Equal(status.ZramGeneratorDevices, want) {
		t.Errorf("generator devices = %+v, want %+v", status.ZramGeneratorDevices, want)
	}
	swaps := generatorMainDevices(t)
	for _, d := range status.ZramGeneratorDevices {
		swaps = append(swaps, d.Device)
	}
	slices.Sort(swaps)
	setups := slices.Sorted(slices.Values(append(slices.Clone(swaps), "zram5")))
	gotSwaps, gotSetups := loadedZramUnits(t, "dev-", ".swap"), loadedZramUnits(t, "systemd-zram-setup@", ".service")
	t.Logf("after a reload the generator made swap units for %v and setup units for %v", gotSwaps, gotSetups)
	if !slices.Equal(gotSwaps, swaps) {
		t.Errorf("swap units for %v, want %v: the status and the generator disagree", gotSwaps, swaps)
	}
	if !slices.Equal(gotSetups, setups) {
		t.Errorf("setup units for %v, want %v, the swap devices and the zram mount", gotSetups, setups)
	}
	t.Logf("zram devices before the reload %v, after it %v", devices, zramDeviceNames(t))
	undo()
}

// zramDeviceNames is the zram devices the kernel has, by name.
func zramDeviceNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(sysBlockDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if isZramName(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// removeAddedZramDevices removes, through zram-control's hot_remove, each
// zram device that is not in before and is not set up.
func removeAddedZramDevices(t *testing.T, before []string) {
	t.Helper()
	for _, name := range zramDeviceNames(t) {
		if slices.Contains(before, name) {
			continue
		}
		if size, err := host.readOptionalUint(filepath.Join(sysBlockDir, name, "disksize")); err != nil || size != 0 {
			t.Errorf("leave %s, which the generator added: disksize %d, %v", name, size, err)
			continue
		}
		if err := os.WriteFile("/sys/class/zram-control/hot_remove", []byte(strings.TrimPrefix(name, "zram")), 0); err != nil {
			t.Errorf("remove %s, which the generator added: %v", name, err)
		}
	}
}

// generatorMainDevices is the swap devices of the main zram-generator file
// the generator reads: the first of /run, /etc, /usr/local/lib and /usr/lib.
func generatorMainDevices(t *testing.T) []string {
	t.Helper()
	for _, dir := range slices.Backward(zramGeneratorDirs) {
		name := dir + "/zram-generator.conf"
		text, ok, err := host.readOptional(name)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		var devices []string
		for _, d := range parseZramGeneratorDevices(name, text) {
			devices = append(devices, d.Device)
		}
		return devices
	}
	return nil
}

// loadedZramUnits is the zram devices of the units systemd has loaded whose
// names are prefix, a zram device and suffix, sorted.
func loadedZramUnits(t *testing.T, prefix, suffix string) []string {
	t.Helper()
	units, err := systemd.ListUnitsWithPrefix(t.Context(), prefix+"zram")
	if err != nil {
		t.Fatal(err)
	}
	var devices []string
	for _, u := range units {
		device, ok := strings.CutSuffix(strings.TrimPrefix(u.Name, prefix), suffix)
		// systemd keeps listing a unit that failed, as zram0's setup service
		// after a stop whose reset failed, though no file defines it now.
		if ok && isZramName(device) && u.LoadState == "loaded" && u.ActiveState != "failed" {
			devices = append(devices, device)
		}
	}
	slices.Sort(devices)
	return devices
}

// assertGivesUpAStopSystemdTimedOut makes the stop of zramswap.service hang
// past a short TimeoutStopSec, as a zramswap stop that outlives its timeout
// does: systemd kills it and must leave the unit failed with zram still
// swapping, and a switch to disk must then end at once, errStopAbandoned,
// and not after the ten minutes of its wait. The switch made LinuxIO's swap
// file before it stopped zram, since zram was the only swap, and the file
// stays on: added swap is never undone. zram is then taken away the way the
// unit would have, with zramswap stop, and the host goes back to disk.
func assertGivesUpAStopSystemdTimedOut(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	switchTo(t, apischema.SwapModeZram)
	t.Cleanup(func() { undoTimedOutStop(t, e) })
	if err := os.MkdirAll(filepath.Dir(zramswapHangDropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	hang := "[Service]\nTimeoutStopSec=5\nExecStop=\nExecStop=/usr/bin/sleep 600\n"
	if err := os.WriteFile(zramswapHangDropIn, []byte(hang), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.reload(ctx); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := SetSwapMode(ctx, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}, logProgress(t))
	elapsed := time.Since(start)
	state, stateErr := e.sys.activeState(ctx, zramService)
	t.Logf("with the stop timed out, the switch to disk ended after %s with %v; %s %s (%v)", elapsed.Round(time.Second), err, zramService, state, stateErr)
	if !errors.Is(err, errStopAbandoned) {
		t.Fatalf("switch to disk with the stop timed out: %v, want %v", err, errStopAbandoned)
	}
	if elapsed > time.Minute {
		t.Errorf("the switch took %s, want it to end soon after the 5 s timeout", elapsed.Round(time.Second))
	}
	if state != "failed" {
		t.Errorf("%s = %q, %v; want failed", zramService, state, stateErr)
	}
	if on, readErr := e.zramIs(true)(); readErr != nil || !on {
		t.Errorf("zram swapping = %v, %v; want it left swapping", on, readErr)
	}
	if listed, _ := swapListed(host, ownSwapFilePath); !listed {
		t.Errorf("%s does not swap after the failed switch, want the swap file it made kept on", ownSwapFilePath)
	}
	undoTimedOutStop(t, e)
	assertDiskMode(t, switchTo(t, apischema.SwapModeDisk))
}

// undoTimedOutStop removes the drop-in that hangs the stop of
// zramswap.service and, when systemd left the unit failed, takes zram away
// with zramswap stop, as the unit's stop would have, and clears the failure.
func undoTimedOutStop(t *testing.T, e *executor) {
	t.Helper()
	ctx := context.WithoutCancel(t.Context())
	removeTestUnitFiles(t, e, nil)
	if state, err := e.sys.activeState(ctx, zramService); err != nil || state != "failed" {
		return
	}
	zramswap, err := hostcmd.LookPath(zramCommand)
	if err != nil {
		t.Errorf("find %s: %v", zramCommand, err)
		return
	}
	// zramswap stop exits non-zero when it cannot unload the zram module
	// right after its swapoff ("Module zram is in use"); what counts is that
	// zram left /proc/swaps.
	if out, err := exec.CommandContext(ctx, zramswap, "stop").CombinedOutput(); err != nil {
		t.Logf("zramswap stop: %v\n%s", err, out)
	}
	if on, err := e.zramIs(true)(); err != nil || on {
		t.Errorf("zram swapping = %v, %v after zramswap stop; want it gone", on, err)
	}
	if err := systemd.ResetFailedUnit(ctx, zramService); err != nil {
		t.Errorf("reset %s: %v", zramService, err)
	}
}

// assertRefusesAnUnknownCompressor writes a zswap compressor the kernel does
// not have, with zswap started: the kernel must refuse it with an error
// compressorRefused takes, which a switch to zswap passes over, and keep the
// compressor it had. Before zswap first starts after a boot the kernel takes
// any name and falls back to its default when zswap starts.
func assertRefusesAnUnknownCompressor(t *testing.T, e *executor) {
	t.Helper()
	before, err := e.sys.readParam(zswapCompressorParam)
	if err != nil {
		t.Fatal(err)
	}
	err = e.sys.writeParam(zswapCompressorParam, "linuxio-integration-test")
	t.Logf("a compressor the kernel does not have: %v", err)
	if !compressorRefused(err) {
		t.Errorf("write an unknown zswap compressor: %v, want ENOENT or EINVAL", err)
	}
	if after, readErr := e.sys.readParam(zswapCompressorParam); readErr != nil || after != before {
		t.Errorf("zswap compressor = %q, %v after the refused write; want %q kept", after, readErr, before)
		_ = e.sys.writeParam(zswapCompressorParam, before)
	}
}

// assertZswapRoundTrip switches to zswap and back to disk, and checks zswap
// live and its tmpfiles entry, which sets it at boot, each way, and with
// zswap on that the kernel refuses a compressor it does not have.
func assertZswapRoundTrip(t *testing.T, e *executor) {
	t.Helper()
	status, _, err := e.sys.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Zswap.Available {
		t.Log("the kernel has no zswap: the zswap round trip is not checked")
		return
	}
	got := switchTo(t, apischema.SwapModeZswap)
	if !got.Zswap.Enabled {
		t.Errorf("zswap = %+v after switching to it, want enabled", got.Zswap)
	}
	if entry, readErr := os.ReadFile(zswapTmpfilesPath); readErr != nil || string(entry) != string(renderZswapTmpfiles(got.Zswap.Compressor)) {
		t.Errorf("%s = %q, %v; want zswap on at boot with %s", zswapTmpfilesPath, entry, readErr, got.Zswap.Compressor)
	}
	assertRefusesAnUnknownCompressor(t, e)
	got = switchTo(t, apischema.SwapModeDisk)
	if got.Zswap.Enabled {
		t.Errorf("zswap = %+v after switching to disk, want off", got.Zswap)
	}
	if entry, readErr := os.ReadFile(zswapTmpfilesPath); readErr != nil || string(entry) != string(renderZswapOffTmpfiles()) {
		t.Errorf("%s = %q, %v; want zswap off at boot", zswapTmpfilesPath, entry, readErr)
	}
}

// assertStartsZramAStopLeftSetUp has a stop of the provider's unit leave
// zram0 set up, as the test VM saw zram-generator's reset fail with EBUSY
// right after a swapoff: zram0 is held open while the unit stops, so the
// reset meets it busy. A switch to zram must then reset the device and
// start zram at the planned size.
func assertStartsZramAStopLeftSetUp(t *testing.T, e *executor, provider apischema.ZramProvider, back apischema.SwapMode) {
	t.Helper()
	ctx := t.Context()
	unit := zramService
	if provider == apischema.ZramProviderGenerator {
		unit = zramGeneratorSwapUnit
	}
	startSmallZram(t, e)
	held, err := os.Open(zramGeneratorDevice)
	if err != nil {
		t.Fatal(err)
	}
	stopErr := e.sys.stopUnit(ctx, unit)
	if stopErr == nil {
		stopErr = waitUntil(ctx, e.waits.swapOn, e.waits.poll, func() (bool, error) {
			off, readErr := e.zramIs(false)()
			if readErr != nil || !off {
				return false, readErr
			}
			state, stateErr := e.sys.activeState(ctx, unit)
			return stateErr == nil && state != "deactivating", stateErr
		})
	}
	if closeErr := held.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if stopErr != nil {
		t.Fatalf("stop %s with zram0 held open: %v", unit, stopErr)
	}
	size, err := host.readOptionalUint("/sys/block/zram0/disksize")
	if err != nil {
		t.Fatal(err)
	}
	setup, _ := e.sys.activeState(ctx, zramGeneratorSetupUnit)
	t.Logf("with zram0 held open, the stop of %s left disksize %d, %s %s", unit, size, zramGeneratorSetupUnit, setup)
	if size == 0 {
		t.Fatalf("zram0 was reset although it was held open: the case this checks did not happen")
	}
	switchTo(t, apischema.SwapModeZram)
	assertZramMode(t, e)
	assertDiskMode(t, switchTo(t, back))
}

// startSmallZram makes the provider swap on a 64 MiB zram device. zram is
// not swapping; its device may still be set up from the stop before, and is
// then reset first.
func startSmallZram(t *testing.T, e *executor) {
	t.Helper()
	ctx := t.Context()
	provider := host.zramProvider(ctx, systemd.GetActiveState)
	unit, path, config := zramService, zramswapConfigPath, renderZramswapConfig(64<<20)
	if provider == apischema.ZramProviderGenerator {
		unit, path, config = zramGeneratorSwapUnit, zramGeneratorConfigPath, renderZramGeneratorConfig(64<<20)
	}
	if err := resetLeftZram(ctx, e, provider); err != nil {
		t.Fatal(err)
	}
	clearZramStartLimit(t)
	if err := e.sys.writeFile(path, config, managedFileMode); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.reload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.sys.restartUnit(ctx, unit); err != nil {
		t.Fatal(err)
	}
	if err := waitUntil(ctx, e.waits.swapOn, e.waits.poll, func() (bool, error) {
		status, _, err := e.sys.status(ctx)
		return err == nil && hasDevice(status.Devices, apischema.SwapDeviceZram) && zramSized(status, 64<<20), err
	}); err != nil {
		t.Fatalf("start a 64 MiB zram device: %v", err)
	}
}

// assertZramMode checks a host in the zram mode: zswap off, zram's sysctls,
// zram at the planned size, no swap file LinuxIO can remove left, and the
// provider's configuration and start at boot.
func assertZramMode(t *testing.T, e *executor) {
	t.Helper()
	got, _, err := e.sys.status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != apischema.SwapModeZram || got.Zswap.Enabled {
		t.Fatalf("status = mode %s zswap %v, want zram without zswap", got.Mode, got.Zswap.Enabled)
	}
	if !got.TunedActive && got.Swappiness != 180 {
		t.Errorf("swappiness = %d, want 180", got.Swappiness)
	}
	if got.PageCluster != 0 || got.WatermarkBoostFactor != 0 || got.WatermarkScaleFactor != 125 {
		t.Errorf("sysctls = page-cluster %d boost %d scale %d, want 0, 0, 125", got.PageCluster, got.WatermarkBoostFactor, got.WatermarkScaleFactor)
	}
	if !zramSized(got, got.ZramPlannedSize) {
		t.Errorf("zram devices = %+v, want the planned %d MiB", got.ZramDevices, got.ZramPlannedSize>>20)
	}
	if removable := zramRemovals(got.SwapFiles); len(removable) > 0 {
		t.Errorf("swap files %v are still there after a switch to zram", removable)
	}
	if got.ZramProvider == apischema.ZramProviderGenerator {
		config, readErr := os.ReadFile(zramGeneratorConfigPath)
		if readErr != nil || !strings.Contains(string(config), "[zram0]\n") || !strings.Contains(string(config), "compression-algorithm = zstd\n") {
			t.Errorf("%s = %q, %v; want a zram0 device with zstd", zramGeneratorConfigPath, config, readErr)
		}
		if state, stateErr := e.sys.activeState(t.Context(), zramGeneratorSetupUnit); stateErr != nil || state != "active" {
			t.Errorf("%s = %q, %v; want active", zramGeneratorSetupUnit, state, stateErr)
		}
		return
	}
	if state := unitFileState(t, zramService); state != "enabled" {
		t.Errorf("%s file state = %q, want enabled", zramService, state)
	}
	config, err := os.ReadFile(zramswapConfigPath)
	if err != nil || !strings.Contains(string(config), "ALGO=zstd\n") {
		t.Errorf("%s = %q, %v; want ALGO=zstd", zramswapConfigPath, config, err)
	}
}

// assertDiskMode checks a host back on disk swap: a swap file LinuxIO can
// remove swaps, with its unit active, no zram swaps, and the provider's
// configuration or start at boot is off.
func assertDiskMode(t *testing.T, got apischema.SwapStatus) {
	t.Helper()
	i := slices.IndexFunc(got.SwapFiles, func(r apischema.SwapFileRow) bool { return r.Active && r.LockedReason == "" })
	if i < 0 {
		t.Fatalf("swap files = %+v, want one LinuxIO can remove swapping", got.SwapFiles)
	}
	assertSwapsAt(t, got.SwapFiles[i].Path, got.SwapFiles[i].Size+uint64(os.Getpagesize()))
	if hasDevice(got.Devices, apischema.SwapDeviceZram) {
		t.Fatalf("devices = %+v, want no zram swapping", got.Devices)
	}
	if got.ZramProvider == apischema.ZramProviderGenerator {
		config, err := os.ReadFile(zramGeneratorConfigPath)
		if err != nil || string(config) != string(renderZramGeneratorOff()) {
			t.Errorf("%s = %q, %v; want the configuration with no device", zramGeneratorConfigPath, config, err)
		}
		return
	}
	if state := unitFileState(t, zramService); state != "disabled" {
		t.Errorf("%s file state = %q, want disabled", zramService, state)
	}
}

func unitFileState(t *testing.T, unit string) string {
	t.Helper()
	state, err := systemd.GetUnitFileState(t.Context(), unit)
	if err != nil {
		t.Fatalf("read the file state of %s: %v", unit, err)
	}
	return state
}
