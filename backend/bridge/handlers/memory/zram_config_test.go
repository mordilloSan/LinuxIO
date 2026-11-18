package memory

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// zramswapAdmins is an /etc/default/zramswap an admin wrote.
const zramswapAdmins = "# Mine\nSIZE=1024\nPRIORITY=10\n"

// generatorAdmins is an /etc/systemd/zram-generator.conf an admin wrote,
// with a zram mount besides the swap device.
const generatorAdmins = "[zram0]\nzram-size = ram / 4\n\n[zram1]\nfs-type = ext4\nmount-point = /var/tmp\n"

func foreignAt(path string) *apischema.ForeignZramConfig {
	return &apischema.ForeignZramConfig{Path: path, CopyPath: path + ".before-linuxio"}
}

// The provider's configuration file is LinuxIO's when it starts with the
// header; one with only comments and blank lines may be replaced too. Any
// other is reported with the path its copy will be kept at.
func TestReadSwapStatusReportsAZramConfigThatIsNotLinuxIOs(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider apischema.ZramProvider
		path     string
		content  string
		want     *apischema.ForeignZramConfig
	}{
		{name: "zram-tools' package default", provider: apischema.ZramProviderTools, path: zramswapConfigPath, content: zramswapPackageDefault},
		{name: "LinuxIO's zramswap", provider: apischema.ZramProviderTools, path: zramswapConfigPath, content: string(renderZramswapConfig(4 * gib))},
		{name: "an admin's zramswap", provider: apischema.ZramProviderTools, path: zramswapConfigPath, content: zramswapAdmins,
			want: foreignAt(zramswapConfigPath)},
		{name: "LinuxIO's header below a setting", provider: apischema.ZramProviderTools, path: zramswapConfigPath,
			content: "SIZE=512\n" + managedHeader, want: foreignAt(zramswapConfigPath)},
		{name: "no generator configuration", provider: apischema.ZramProviderGenerator},
		{name: "blank lines only", provider: apischema.ZramProviderGenerator, path: zramGeneratorConfigPath, content: "\n  \n"},
		{name: "LinuxIO's off file", provider: apischema.ZramProviderGenerator, path: zramGeneratorConfigPath, content: string(renderZramGeneratorOff())},
		{name: "an admin's generator file", provider: apischema.ZramProviderGenerator, path: zramGeneratorConfigPath, content: generatorAdmins,
			want: foreignAt(zramGeneratorConfigPath)},
		{name: "the other tool's file", provider: apischema.ZramProviderTools, path: zramGeneratorConfigPath, content: generatorAdmins},
		{name: "no provider", path: zramswapConfigPath, content: zramswapAdmins},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _ := diskHost(t)
			if test.path != "" {
				f.write(test.path, test.content)
			}
			got, _, err := f.host().readSwapStatus(test.provider)
			if err != nil {
				t.Fatal(err)
			}
			if !equalForeign(got.ForeignZramConfig, test.want) {
				t.Fatalf("foreign config = %+v, want %+v", got.ForeignZramConfig, test.want)
			}
		})
	}
}

func equalForeign(a, b *apischema.ForeignZramConfig) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// A file the session user cannot read may hold anything, so it counts as
// not LinuxIO's: the dialog asks, and the change, which reads it as root,
// decides from what is there.
func TestReadSwapStatusCountsAZramConfigItCannotReadAsNotLinuxIOs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	f, _ := diskHost(t)
	if err := os.Chmod(filepath.Join(f.root, zramswapConfigPath), 0); err != nil {
		t.Fatal(err)
	}
	got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if !equalForeign(got.ForeignZramConfig, foreignAt(zramswapConfigPath)) {
		t.Fatalf("foreign config = %+v, want the file reported", got.ForeignZramConfig)
	}
}

// A run that would replace a configuration file that is not LinuxIO's needs
// the request to name it: to zram with either tool, and away from zram with
// zram-generator, which writes its file then. zram-tools only disables its
// service on the way out.
func TestPlanSwapModeAsksBeforeReplacingAZramConfig(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider apischema.ZramProvider
		from     apischema.SwapMode
		req      apischema.SwapModeRequest
		refused  bool
	}{
		{name: "to zram, not confirmed", provider: apischema.ZramProviderTools, from: apischema.SwapModeDisk,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeZram}, refused: true},
		{name: "to zram, another file confirmed", provider: apischema.ZramProviderTools, from: apischema.SwapModeDisk,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: "/etc/default/other"}, refused: true},
		{name: "to zram, confirmed", provider: apischema.ZramProviderTools, from: apischema.SwapModeDisk,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: zramswapConfigPath}},
		{name: "to disk with zram-tools", provider: apischema.ZramProviderTools, from: apischema.SwapModeZram,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}},
		{name: "to disk with zram-generator, not confirmed", provider: apischema.ZramProviderGenerator, from: apischema.SwapModeZram,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}, refused: true},
		{name: "to zswap with zram-generator, not confirmed", provider: apischema.ZramProviderGenerator, from: apischema.SwapModeDisk,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}, refused: true},
		{name: "to zswap with zram-generator, confirmed", provider: apischema.ZramProviderGenerator, from: apischema.SwapModeDisk,
			req: apischema.SwapModeRequest{Mode: apischema.SwapModeZswap, ConfirmReplace: zramGeneratorConfigPath}},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := planView(test.from)
			view.status.ZramProvider = test.provider
			path := zramswapConfigPath
			if test.provider == apischema.ZramProviderGenerator {
				path = zramGeneratorConfigPath
			}
			view.status.ForeignZramConfig = foreignAt(path)

			got, err := planSwapMode(view, test.req)
			if !test.refused {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), path) {
				t.Fatalf("plan = %+v, err = %v; want a conflict naming %s", got, err, path)
			}
		})
	}
}

// With zram-generator, a device another of its files defines comes back at
// every boot, so leaving zram is refused, naming the first such file;
// choosing zram is not. zram-tools reads none of those files.
func TestPlanSwapModeRefusesToLeaveZramAnotherGeneratorFileDefines(t *testing.T) {
	devices := []apischema.ZramGeneratorDevice{
		{Path: "/run/systemd/zram-generator.conf", Device: "zram0", ZramSize: "ram / 8"},
		{Path: "/etc/systemd/zram-generator.conf.d/50-big.conf", Device: "zram1"},
	}
	for _, target := range []apischema.SwapMode{apischema.SwapModeDisk, apischema.SwapModeZswap} {
		view := planView(apischema.SwapModeZram)
		view.status.ZramProvider = apischema.ZramProviderGenerator
		view.status.ZramGeneratorDevices = devices
		got, err := planSwapMode(view, apischema.SwapModeRequest{Mode: target})
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), devices[0].Path) {
			t.Errorf("%s: plan = %+v, err = %v; want a conflict naming %s", target, got, err, devices[0].Path)
		}
	}

	view := planView(apischema.SwapModeDisk)
	view.status.ZramProvider = apischema.ZramProviderGenerator
	view.status.ZramGeneratorDevices = devices
	if _, err := planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Errorf("to zram: %v", err)
	}

	view = planView(apischema.SwapModeZram)
	view.status.ZramProvider = apischema.ZramProviderTools
	view.status.ZramGeneratorDevices = devices
	if _, err := planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk}); err != nil {
		t.Errorf("to disk with zram-tools: %v", err)
	}
}

// An admin's /etc/default/zramswap is not replaced unless the request names
// it, and is refused before anything changes. Once named, a copy is kept
// beside it with its mode, over an older copy, and then the file is
// replaced.
func TestSetSwapModeKeepsACopyOfAZramswapFileThatIsNotLinuxIOs(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zramswapConfigPath, zramswapAdmins)
	if err := os.Chmod(filepath.Join(f.root, zramswapConfigPath), 0o600); err != nil {
		t.Fatal(err)
	}
	copyPath := zramswapConfigPath + ".before-linuxio"
	f.write(copyPath, "an older copy\n")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), zramswapConfigPath) {
		t.Fatalf("err = %v, want a conflict naming %s", err, zramswapConfigPath)
	}
	assertCalls(t, sys.calls, nil)
	if got := f.read(zramswapConfigPath); got != zramswapAdmins {
		t.Fatalf("zramswap config = %q, want it untouched", got)
	}

	_, err = sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: zramswapConfigPath})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.read(copyPath); got != zramswapAdmins {
		t.Errorf("copy = %q, want the admin's file", got)
	}
	info, err := os.Stat(filepath.Join(f.root, copyPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("copy mode = %v, want the file's 0600", info.Mode().Perm())
	}
	if got := f.read(zramswapConfigPath); got != string(renderZramswapConfig(16308408*kib/2)) {
		t.Errorf("zramswap config = %q, want LinuxIO's", got)
	}
	i := slices.Index(sys.calls, "write "+copyPath)
	if i < 0 || i+1 >= len(sys.calls) || sys.calls[i+1] != "write "+zramswapConfigPath {
		t.Errorf("calls = %v, want the copy written just before the file", sys.calls)
	}
}

// A failed run puts the admin's file back and keeps the copy.
func TestSetSwapModeKeepsTheCopyWhenTheRunFails(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zramswapConfigPath, zramswapAdmins)
	sys.failures["set "+swappinessParam+"=180"] = errors.New("read-only")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: zramswapConfigPath})
	// The error says where the copy the rollback left is.
	if err == nil || !strings.Contains(err.Error(), zramswapConfigPath+".before-linuxio") {
		t.Fatalf("err = %v, want the copy named", err)
	}
	if got := f.read(zramswapConfigPath); got != zramswapAdmins {
		t.Errorf("zramswap config = %q, want the admin's back", got)
	}
	if got := f.read(zramswapConfigPath + ".before-linuxio"); got != zramswapAdmins {
		t.Errorf("copy = %q, want it kept", got)
	}
}

// LinuxIO's own file and the package default are replaced without asking
// and without a copy.
func TestSetSwapModeReplacesLinuxIOsOwnZramConfigWithoutACopy(t *testing.T) {
	_, sys := diskHost(t)

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(sys.calls, func(call string) bool { return strings.HasSuffix(call, ".before-linuxio") }) {
		t.Fatalf("calls = %v, want no copy", sys.calls)
	}
}

// Leaving zram with zram-generator writes its file with no device, so an
// admin's file there needs the same confirmation, and is kept as a copy.
func TestSetSwapModeWithTheGeneratorKeepsACopyOfAnAdminsFileOnTheWayOut(t *testing.T) {
	f, sys := generatorZramHost(t)
	f.write(zramGeneratorConfigPath, generatorAdmins)

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), zramGeneratorConfigPath) {
		t.Fatalf("err = %v, want a conflict naming %s", err, zramGeneratorConfigPath)
	}
	assertCalls(t, sys.calls, nil)

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk, ConfirmReplace: zramGeneratorConfigPath}); err != nil {
		t.Fatal(err)
	}
	if got := f.read(zramGeneratorConfigPath + ".before-linuxio"); got != generatorAdmins {
		t.Errorf("copy = %q, want the admin's file", got)
	}
	if got := f.read(zramGeneratorConfigPath); got != string(renderZramGeneratorOff()) {
		t.Errorf("generator config = %q, want the off file", got)
	}
}

// A device another generator file defines keeps a switch away from zram
// from starting at all.
func TestSetSwapModeWithTheGeneratorRefusesToLeaveZramADropInDefines(t *testing.T) {
	f, sys := generatorZramHost(t)
	const dropIn = "/etc/systemd/zram-generator.conf.d/50-big.conf"
	f.write(dropIn, "[zram1]\nzram-size = 2048\n")

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), dropIn) {
		t.Fatalf("err = %v, want a conflict naming %s", err, dropIn)
	}
	assertCalls(t, sys.calls, nil)
}

// The generator's other configuration, read as upstream does: one main
// file, the last of /usr/lib, /usr/local/lib, /etc and /run that exists, of
// which LinuxIO's in /etc hides the two before it, then the drop-ins by file
// name, a later folder's replacing an earlier folder's of the same name and
// a link to /dev/null hiding it. Each [zramN] section is listed with its
// zram-size as written.
func TestReadSwapStatusListsTheDevicesOtherGeneratorFilesDefine(t *testing.T) {
	f, _ := generatorDiskHost(t)
	const (
		vendorDir = "/usr/lib/systemd/zram-generator.conf.d/"
		etcDir    = "/etc/systemd/zram-generator.conf.d/"
		runDir    = "/run/systemd/zram-generator.conf.d/"
	)
	f.write("/usr/lib/systemd/zram-generator.conf", "[zram0]\nzram-size = min(ram, 8192)\n")
	f.write("/run/systemd/zram-generator.conf", "# Set at boot\n[zram0]\nzram-size = ram / 8\ncompression-algorithm = lz4\n")
	f.write(vendorDir+"10-vendor.conf", "[zram1]\nzram-size = 2048\n")
	f.symlink("/dev/null", etcDir+"10-vendor.conf")
	f.write(vendorDir+"20-other.conf", "[zram2]\nzram-size = 1024\n")
	f.write(runDir+"20-other.conf", "[zram3]\nzram-size = ram\n")
	f.write(etcDir+"30-admin.conf", "[zram4]\nswap-priority = 5\n\n[zram-generator]\nzram-size = 1\n")
	f.write(etcDir+"40-none.conf", "# nothing\n[zram0]\n")
	f.write(etcDir+"45-settings.conf", "[zram-generator]\nzram-size = 1\n")
	f.write(etcDir+".hidden.conf", "[zram5]\n")
	f.write(etcDir+"60-not-conf.txt", "[zram6]\n")
	f.write("/srv/elsewhere.conf", "[zram7]\n")
	f.symlink("/srv/elsewhere.conf", etcDir+"70-link.conf")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderGenerator)
	if err != nil {
		t.Fatal(err)
	}
	want := []apischema.ZramGeneratorDevice{
		{Path: "/run/systemd/zram-generator.conf", Device: "zram0", ZramSize: "ram / 8"},
		{Path: runDir + "20-other.conf", Device: "zram3", ZramSize: "ram"},
		{Path: etcDir + "30-admin.conf", Device: "zram4"},
		{Path: etcDir + "40-none.conf", Device: "zram0"},
	}
	if !slices.Equal(got.ZramGeneratorDevices, want) {
		t.Fatalf("generator devices =\n%+v\nwant\n%+v", got.ZramGeneratorDevices, want)
	}
}

// Without zram-generator installed nothing reads those files, and the list
// is empty, not null.
func TestReadSwapStatusListsNoGeneratorDevicesWithoutTheGenerator(t *testing.T) {
	f, _ := diskHost(t)
	f.write("/etc/systemd/zram-generator.conf.d/50-big.conf", "[zram1]\n")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
	if err != nil {
		t.Fatal(err)
	}
	if got.ZramGeneratorDevices == nil || len(got.ZramGeneratorDevices) != 0 {
		t.Fatalf("generator devices = %#v, want an empty list", got.ZramGeneratorDevices)
	}
}

// A drop-in the session user cannot read is skipped; the status still
// reads.
func TestReadSwapStatusSkipsAGeneratorFileItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	f, _ := generatorDiskHost(t)
	const dropIn = "/etc/systemd/zram-generator.conf.d/50-big.conf"
	f.write(dropIn, "[zram1]\n")
	f.write("/etc/systemd/zram-generator.conf.d/60-ok.conf", "[zram2]\n")
	if err := os.Chmod(filepath.Join(f.root, dropIn), 0); err != nil {
		t.Fatal(err)
	}

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderGenerator)
	if err != nil {
		t.Fatal(err)
	}
	want := []apischema.ZramGeneratorDevice{{Path: "/etc/systemd/zram-generator.conf.d/60-ok.conf", Device: "zram2"}}
	if !slices.Equal(got.ZramGeneratorDevices, want) {
		t.Fatalf("generator devices = %+v, want %+v", got.ZramGeneratorDevices, want)
	}
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// zramswapStock is an /etc/default/zramswap as a package ships it, with
// settings of its own.
const zramswapStock = "# Compression algorithm selection\nALGO=lz4\nPERCENT=50\n"

// dpkgStatus is a /var/lib/dpkg/status whose zram-tools stanza records
// conffile as the package shipped it, with a checksum of content and the
// given flags after it.
func dpkgStatus(conffile, content, flags string) string {
	entry := " " + conffile + " " + md5Hex([]byte(content))
	if flags != "" {
		entry += " " + flags
	}
	return "Package: bash\nStatus: install ok installed\nConffiles:\n /etc/bash.bashrc 89269e1298235f1b12b4c16e4065ad0d\nDescription: GNU Bourne Again SHell\n\n" +
		"Package: zram-tools\nStatus: install ok installed\nVersion: 0.3.5-1\nConffiles:\n" + entry + "\n /etc/other.conf d41d8cd98f00b204e9800998ecf8427e\nDescription: utilities for working with zram\n"
}

// A configuration file identical to the one its package installed is no
// admin's: on a dpkg host the package's checksum of it, in the Conffiles
// field of /var/lib/dpkg/status, says so. A file that differs, an entry
// marked obsolete, and no record at all leave the rule as it is.
func TestReadSwapStatusTakesAnUnchangedPackageConffileAsLinuxIOs(t *testing.T) {
	for _, test := range []struct {
		name, path, content, status string
		want                        *apischema.ForeignZramConfig
	}{
		{name: "zramswap as installed", path: zramswapConfigPath, content: zramswapStock,
			status: dpkgStatus(zramswapConfigPath, zramswapStock, "")},
		{name: "the generator's file as installed", path: zramGeneratorConfigPath, content: generatorAdmins,
			status: dpkgStatus(zramGeneratorConfigPath, generatorAdmins, "")},
		{name: "zramswap changed since", path: zramswapConfigPath, content: zramswapStock + "SIZE=512\n",
			status: dpkgStatus(zramswapConfigPath, zramswapStock, ""), want: foreignAt(zramswapConfigPath)},
		{name: "an entry marked obsolete", path: zramswapConfigPath, content: zramswapStock,
			status: dpkgStatus(zramswapConfigPath, zramswapStock, "obsolete"), want: foreignAt(zramswapConfigPath)},
		{name: "no record of the file", path: zramswapConfigPath, content: zramswapStock,
			status: dpkgStatus("/etc/default/other", zramswapStock, ""), want: foreignAt(zramswapConfigPath)},
		{name: "no dpkg status", path: zramswapConfigPath, content: zramswapStock, want: foreignAt(zramswapConfigPath)},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _ := diskHost(t)
			f.write(test.path, test.content)
			if test.status != "" {
				f.write(dpkgStatusPath, test.status)
			}
			provider := apischema.ZramProviderTools
			if test.path == zramGeneratorConfigPath {
				provider = apischema.ZramProviderGenerator
			}
			got, _, err := f.host().readSwapStatus(provider)
			if err != nil {
				t.Fatal(err)
			}
			if !equalForeign(got.ForeignZramConfig, test.want) {
				t.Fatalf("foreign config = %+v, want %+v", got.ForeignZramConfig, test.want)
			}
		})
	}
}

// The first switch to zram on a host where zram-tools was only installed
// replaces the package's file without asking and keeps no copy of it.
func TestSetSwapModeReplacesAnUnchangedPackageConffileWithoutACopy(t *testing.T) {
	f, sys := diskHost(t)
	f.write(zramswapConfigPath, zramswapStock)
	f.write(dpkgStatusPath, dpkgStatus(zramswapConfigPath, zramswapStock, ""))

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Fatal(err)
	}
	if f.exists(zramswapConfigPath + ".before-linuxio") {
		t.Error("a copy of the package's file was kept")
	}
}

// Comments in zram-generator's INI file may start with ';', and a header or
// comments written with a byte order mark or CRLF line ends are still read
// as such. In /etc/default/zramswap, a shell file, ';' starts no comment.
func TestNotLinuxIOsReadsCommentsAsTheToolsDo(t *testing.T) {
	crlfHeader := strings.TrimSuffix(managedHeader, "\n") + "\r\n"
	for _, test := range []struct {
		name, path, text string
		want             bool
	}{
		{name: "INI comments", path: zramGeneratorConfigPath, text: "; off\n# also off\n\n"},
		{name: "';' in a shell file", path: zramswapConfigPath, text: "; SIZE=1\n", want: true},
		{name: "a BOM and CRLF comments", path: zramswapConfigPath, text: "\ufeff# Size\r\n\r\n#SIZE=1\r\n"},
		{name: "a BOM before the header", path: zramswapConfigPath, text: "\ufeff" + managedHeader + "SIZE=1\n"},
		{name: "the header with CRLF", path: zramGeneratorConfigPath, text: crlfHeader + "[zram0]\r\n"},
		{name: "a setting", path: zramGeneratorConfigPath, text: "\ufeff[zram0]\r\n", want: true},
	} {
		if got := notLinuxIOs(test.path, test.text); got != test.want {
			t.Errorf("%s: notLinuxIOs = %v, want %v", test.name, got, test.want)
		}
	}
}

// A section that sets up a zram mount, not swap, does not come back as swap
// at boot, so it is not listed and does not hold a switch away from zram.
func TestReadSwapStatusListsOnlyTheSwapDevicesOtherGeneratorFilesDefine(t *testing.T) {
	f, _ := generatorDiskHost(t)
	const etcDir = "/etc/systemd/zram-generator.conf.d/"
	f.write(etcDir+"10-mount.conf", "[zram1]\nfs-type = ext4\nmount-point = /var/compressed\n")
	f.write(etcDir+"20-tmp.conf", "[zram2]\nmount-point = /var/tmp\n")
	f.write(etcDir+"30-fs.conf", "[zram3]\nfs-type = ext2\n")
	f.write(etcDir+"40-swap.conf", "[zram4]\nfs-type = swap\nzram-fraction = 0.25\nmax-zram-size = 2048\n")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderGenerator)
	if err != nil {
		t.Fatal(err)
	}
	want := []apischema.ZramGeneratorDevice{{Path: etcDir + "40-swap.conf", Device: "zram4", ZramFraction: "0.25", MaxZramSize: "2048"}}
	if !slices.Equal(got.ZramGeneratorDevices, want) {
		t.Fatalf("generator devices = %+v, want %+v", got.ZramGeneratorDevices, want)
	}
}

// A zram mount another generator file defines lets a switch from disk to
// zswap through.
func TestSetSwapModeWithTheGeneratorIgnoresAZramMount(t *testing.T) {
	f, sys := generatorDiskHost(t)
	f.write("/etc/systemd/zram-generator.conf.d/10-mount.conf", "[zram1]\nmount-point = /var/compressed\n")

	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZswap}); err != nil {
		t.Fatal(err)
	}
}

// A section header may carry a comment after it. Upstream's rust-ini, as
// zram-generator builds it, rejects the whole file for that ("doesn't
// support inline comment") and the generator then makes no device at all;
// counting the section errs towards refusing a switch away from zram.
func TestParseZramGeneratorDevicesReadsAHeaderWithAComment(t *testing.T) {
	got := parseZramGeneratorDevices("/x.conf", "\ufeff[zram1]   # note\nzram-size = 512\n[zram2] ; note\n[zram3] trailing\n")
	want := []apischema.ZramGeneratorDevice{
		{Path: "/x.conf", Device: "zram1", ZramSize: "512"},
		{Path: "/x.conf", Device: "zram2"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("devices = %+v, want %+v", got, want)
	}
}

// The refusal says how to turn the file off: a package's drop-in with a
// link to /dev/null of its name in /etc, any other by removing the device.
func TestPlanSwapModeSaysHowToTurnAGeneratorFileOff(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{path: "/usr/lib/systemd/zram-generator.conf.d/50-big.conf", want: "link /etc/systemd/zram-generator.conf.d/50-big.conf to /dev/null"},
		{path: "/usr/local/lib/systemd/zram-generator.conf.d/50-big.conf", want: "link /etc/systemd/zram-generator.conf.d/50-big.conf to /dev/null"},
		{path: "/etc/systemd/zram-generator.conf.d/50-big.conf", want: "take the device out of it"},
		{path: "/run/systemd/zram-generator.conf", want: "take the device out of it"},
	} {
		view := planView(apischema.SwapModeZram)
		view.status.ZramProvider = apischema.ZramProviderGenerator
		view.status.ZramGeneratorDevices = []apischema.ZramGeneratorDevice{{Path: test.path, Device: "zram1"}}
		_, err := planSwapMode(view, apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
		if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: err = %v, want a conflict that says %q", test.path, err, test.want)
		}
	}
}

// A generator file root cannot read while it plans a switch away from zram
// may bring zram back at boot, so the switch is refused, naming it: a
// drop-in, the main file in /run, and a file larger than 1 MiB. A drop-in
// folder that cannot be listed, or that is not a folder, is skipped, as
// upstream skips it.
func TestSetSwapModeWithTheGeneratorRefusesWhatItCannotRead(t *testing.T) {
	const dropIn = "/etc/systemd/zram-generator.conf.d/50-big.conf"
	for _, test := range []struct {
		name, path string
		refused    bool
		setUp      func(f fixture, path string)
	}{
		{name: "drop-in", path: dropIn, refused: true, setUp: func(f fixture, path string) {
			f.write(path, "[zram1]\n")
			f.unreadable(path)
		}},
		{name: "main file in /run", path: zramGeneratorRunConfigPath, refused: true, setUp: func(f fixture, path string) {
			f.write(path, "[zram1]\n")
			f.unreadable(path)
		}},
		{name: "a file over 1 MiB", path: dropIn, refused: true, setUp: func(f fixture, path string) {
			f.write(path, "[zram1]\n"+strings.Repeat("#", 1<<20)+"\n")
		}},
		{name: "folder", path: "/run/systemd/zram-generator.conf.d", setUp: func(f fixture, path string) {
			f.write(path+"/50-big.conf", "[zram1]\n")
			f.unreadable(path)
		}},
		{name: "not a folder", path: "/etc/systemd/zram-generator.conf.d", setUp: func(f fixture, path string) {
			f.write(path, "[zram1]\n")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name != "a file over 1 MiB" && test.name != "not a folder" && os.Geteuid() == 0 {
				t.Skip("root reads any file")
			}
			f, sys := generatorZramHost(t)
			test.setUp(f, test.path)

			_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeDisk})
			if !test.refused {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), test.path+" cannot be read") {
				t.Fatalf("err = %v, want a conflict that %s cannot be read", err, test.path)
			}
			assertCalls(t, sys.calls, nil)
		})
	}
}

// unreadable makes name, a file or a folder, one nobody but root can read,
// until the test ends.
func (f fixture) unreadable(name string) {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.Chmod(path, 0); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

// The status skips what it cannot read, and its Debug line says why.
func TestReadSwapStatusLogsTheGeneratorFileItSkips(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f, _ := generatorDiskHost(t)
	const dropIn = "/etc/systemd/zram-generator.conf.d/50-big.conf"
	f.write(dropIn, "[zram1]\n")
	f.unreadable(dropIn)

	if _, _, err := f.host().readSwapStatus(apischema.ZramProviderGenerator); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "path="+dropIn+" error=") {
		t.Fatalf("logs = %q, want the skipped file with its error", logs.String())
	}
}

// After a package was renamed, the old one can stay in config-files state,
// listing the same conffile with its old checksum: only an installed
// package's record counts.
func TestReadSwapStatusTakesTheInstalledPackagesConffileRecord(t *testing.T) {
	stanza := func(pkg, status, content string) string {
		return "Package: " + pkg + "\nStatus: " + status + "\nConffiles:\n " + zramswapConfigPath + " " + md5Hex([]byte(content)) + "\nDescription: zram\n"
	}
	const old = "ALGO=lzo\n"
	for _, test := range []struct {
		name, status, content string
		want                  *apischema.ForeignZramConfig
	}{
		{name: "the installed package's file", content: zramswapStock,
			status: stanza("zram-config", "deinstall ok config-files", old) + "\n" + stanza("zram-tools", "install ok installed", zramswapStock)},
		{name: "the removed package's file", content: old, want: foreignAt(zramswapConfigPath),
			status: stanza("zram-config", "deinstall ok config-files", old) + "\n" + stanza("zram-tools", "install ok installed", zramswapStock)},
		{name: "no installed package", content: old, want: foreignAt(zramswapConfigPath),
			status: stanza("zram-config", "deinstall ok config-files", old)},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _ := diskHost(t)
			f.write(zramswapConfigPath, test.content)
			f.write(dpkgStatusPath, test.status)
			got, _, err := f.host().readSwapStatus(apischema.ZramProviderTools)
			if err != nil {
				t.Fatal(err)
			}
			if !equalForeign(got.ForeignZramConfig, test.want) {
				t.Fatalf("foreign config = %+v, want %+v", got.ForeignZramConfig, test.want)
			}
		})
	}
}

// confirm_replace names one of the two zram configuration files or nothing;
// anything else is refused as a bad request before the change waits for the
// lock.
func TestSetSwapModeRefusesAConfirmationOfAnotherFile(t *testing.T) {
	_, sys := diskHost(t)
	mutationMu.Lock()
	defer mutationMu.Unlock()

	_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: "/etc/passwd"})
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("err = %v, want invalid arguments", err)
	}
	assertCalls(t, sys.calls, nil)
}

// The Info line of a change names the configuration file it replaced and
// the copy it kept, only when it did.
func TestSetSwapModeLogsTheConfigurationFileItReplaced(t *testing.T) {
	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, sys := diskHost(t)
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "zram_config") {
		t.Fatalf("logs = %q, want no configuration file named", logs.String())
	}

	logs.Reset()
	f, sys := diskHost(t)
	f.write(zramswapConfigPath, zramswapAdmins)
	if _, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: zramswapConfigPath}); err != nil {
		t.Fatal(err)
	}
	want := "zram_config_replaced=" + zramswapConfigPath + " zram_config_copy=" + zramswapConfigPath + ".before-linuxio"
	if !strings.Contains(logs.String(), `msg="swap mode changed"`) || !strings.Contains(logs.String(), want) {
		t.Fatalf("logs = %q, want %q on the change's Info line", logs.String(), want)
	}
}

// Something at the copy's name that is not a regular file stops the run
// before the file is replaced.
func TestSetSwapModeStopsWhenTheCopyCannotBeWritten(t *testing.T) {
	for _, test := range []string{"folder", "link"} {
		t.Run(test, func(t *testing.T) {
			f, sys := diskHost(t)
			f.write(zramswapConfigPath, zramswapAdmins)
			copyPath := zramswapConfigPath + ".before-linuxio"
			if test == "folder" {
				f.mkdir(copyPath)
			} else {
				f.write("/srv/elsewhere", "")
				f.symlink(filepath.Join(f.root, "/srv/elsewhere"), copyPath)
			}

			_, err := sys.executor(nil).setSwapMode(t.Context(), apischema.SwapModeRequest{Mode: apischema.SwapModeZram, ConfirmReplace: zramswapConfigPath})
			if err == nil || !strings.Contains(err.Error(), copyPath) {
				t.Fatalf("err = %v, want the copy named", err)
			}
			if slices.Contains(sys.calls, "write "+zramswapConfigPath) || f.read(zramswapConfigPath) != zramswapAdmins {
				t.Fatalf("calls = %v, want the file left as it was", sys.calls)
			}
			if test == "link" && f.read("/srv/elsewhere") != "" {
				t.Error("the copy was written through the link")
			}
		})
	}
}

// Fedora's default in /usr/lib is not listed when neither /etc nor /run has
// a main file: the file LinuxIO writes in /etc takes its place.
func TestReadSwapStatusDoesNotListTheVendorMainFile(t *testing.T) {
	f, _ := generatorDiskHost(t)
	f.write("/usr/lib/systemd/zram-generator.conf", "[zram0]\nzram-size = min(ram, 8192)\n")

	got, _, err := f.host().readSwapStatus(apischema.ZramProviderGenerator)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ZramGeneratorDevices) != 0 {
		t.Fatalf("generator devices = %+v, want none", got.ZramGeneratorDevices)
	}
}
