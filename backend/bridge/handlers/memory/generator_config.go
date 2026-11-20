package memory

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// zramGeneratorDirs are the folders zram-generator reads its configuration
// from, in rising precedence, as upstream's locate_fragments (src/config.rs)
// lists them.
var zramGeneratorDirs = []string{"/usr/lib/systemd", "/usr/local/lib/systemd", "/etc/systemd", "/run/systemd"}

const (
	zramGeneratorRunConfigPath = "/run/systemd/zram-generator.conf"
	zramGeneratorDropInDir     = "zram-generator.conf.d"
	// zramGeneratorEtcDropIns is where a link to /dev/null hides a package's
	// drop-in of the same name.
	zramGeneratorEtcDropIns = "/etc/systemd/" + zramGeneratorDropInDir
)

// maxGeneratorFile is the largest zram-generator file read; a larger one
// counts as one that cannot be read.
const maxGeneratorFile = 1 << 20

// unreadableFile is a zram-generator file that could not be read, and why.
type unreadableFile struct {
	path string
	err  error
}

// zramGeneratorDevices is what the status lists: the swap devices
// readZramGeneratorDevices finds, while zram-generator is installed. The
// status is an unprivileged read, so a file it cannot read is left out with
// a Debug log; a change refuses instead (generatorRefusal).
func (h hostRoot) zramGeneratorDevices() []apischema.ZramGeneratorDevice {
	devices := []apischema.ZramGeneratorDevice{}
	if !h.exists(ZramGeneratorPath) {
		return devices
	}
	found, unreadable := h.readZramGeneratorDevices()
	for _, file := range unreadable {
		slog.Debug("read zram-generator configuration failed", "path", file.path, "error", file.err)
	}
	return append(devices, found...)
}

// readZramGeneratorDevices reads the zram swap devices zram-generator's
// configuration files other than LinuxIO's define, and names the files it
// could not read. Upstream reads one main file, the first of /run, /etc,
// /usr/local/lib and /usr/lib that exists, so LinuxIO's in /etc hides the
// two after it and only /run's hides LinuxIO's; then the drop-ins, *.conf in
// zram-generator.conf.d under the same four folders, by file name whatever
// folder they are in, a later folder's file taking the place of an earlier
// folder's file of the same name, and a link to /dev/null hiding it
// (liboverdrop's scan).
func (h hostRoot) readZramGeneratorDevices() (devices []apischema.ZramGeneratorDevice, unreadable []unreadableFile) {
	var files []string
	if h.exists(zramGeneratorRunConfigPath) {
		files = append(files, zramGeneratorRunConfigPath)
	}
	files = append(files, h.zramGeneratorDropIns()...)
	for _, name := range files {
		text, err := h.readGeneratorFile(name)
		if err != nil {
			unreadable = append(unreadable, unreadableFile{path: name, err: err})
			continue
		}
		devices = append(devices, parseZramGeneratorDevices(name, text)...)
	}
	return devices, unreadable
}

// readGeneratorFile reads a zram-generator file of at most maxGeneratorFile
// bytes.
func (h hostRoot) readGeneratorFile(name string) (string, error) {
	f, err := os.Open(h.path(name))
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxGeneratorFile+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxGeneratorFile {
		return "", fmt.Errorf("larger than %d bytes", maxGeneratorFile)
	}
	return string(data), nil
}

// zramGeneratorDropIns lists the drop-ins zram-generator reads, in the order
// it reads them. As liboverdrop does, an entry that is not a regular file is
// skipped, a link to /dev/null also hiding the earlier folders' file of its
// name, and so are names that start with a dot or do not end in .conf; a
// folder that cannot be listed, or is not a folder, is skipped too, with a
// Debug log.
func (h hostRoot) zramGeneratorDropIns() []string {
	byName := map[string]string{}
	for _, dir := range zramGeneratorDirs {
		dir = filepath.Join(dir, zramGeneratorDropInDir)
		if err := h.scanZramGeneratorDropIns(dir, byName); err != nil {
			slog.Debug("list zram-generator drop-ins failed", "path", dir, "error", err)
		}
	}
	var paths []string
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		paths = append(paths, byName[name])
	}
	return paths
}

// scanZramGeneratorDropIns records in byName, by file name, the drop-ins of
// one folder, over those of the folders before it. A folder that does not
// exist has none.
func (h hostRoot) scanZramGeneratorDropIns(dir string, byName map[string]string) error {
	entries, err := os.ReadDir(h.path(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || filepath.Ext(name) != ".conf" {
			continue
		}
		file := filepath.Join(dir, name)
		switch {
		case entry.Type().IsRegular():
			byName[name] = file
		case entry.Type()&fs.ModeSymlink != 0:
			if target, err := os.Readlink(h.path(file)); err == nil && target == os.DevNull {
				delete(byName, name)
			}
		}
	}
	return nil
}

// generatorSection is one [zramN] section of a zram-generator file; keys
// points each key that says whether it is swap, and how big it is, at the
// field that holds it.
type generatorSection struct {
	device             apischema.ZramGeneratorDevice
	fsType, mountPoint string
	keys               map[string]*string
}

// parseZramGeneratorDevices reads the [zramN] sections of a zram-generator
// configuration file that set up swap, with the size settings each writes,
// as written. A section with a mount-point, or an fs-type other than swap,
// is a zram mount and is left out. A section named twice is one device, as
// the generator merges it. A header may carry a comment after it, which
// upstream's parser rejects with the whole file; counting the section errs
// towards refusing a switch away from zram.
func parseZramGeneratorDevices(file, text string) []apischema.ZramGeneratorDevice {
	var sections []*generatorSection
	var current *generatorSection
	// rust-ini reads past a byte order mark at the start of a file.
	for line := range strings.Lines(strings.TrimPrefix(text, "\ufeff")) {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[':
			current = nil
			name, ok := sectionName(line)
			if !ok || !isZramName(name) {
				continue
			}
			i := slices.IndexFunc(sections, func(s *generatorSection) bool { return s.device.Device == name })
			if i < 0 {
				sections = append(sections, newGeneratorSection(file, name))
				i = len(sections) - 1
			}
			current = sections[i]
		case current != nil:
			current.set(line)
		}
	}
	var devices []apischema.ZramGeneratorDevice
	for _, s := range sections {
		if s.mountPoint == "" && (s.fsType == "" || s.fsType == "swap") {
			devices = append(devices, s.device)
		}
	}
	return devices
}

func newGeneratorSection(file, name string) *generatorSection {
	s := &generatorSection{device: apischema.ZramGeneratorDevice{Path: file, Device: name}}
	s.keys = map[string]*string{
		"zram-size":     &s.device.ZramSize,
		"zram-fraction": &s.device.ZramFraction,
		"max-zram-size": &s.device.MaxZramSize,
		"fs-type":       &s.fsType,
		"mount-point":   &s.mountPoint,
	}
	return s
}

// set records a "key = value" line the section cares about.
func (s *generatorSection) set(line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	if field := s.keys[strings.TrimSpace(key)]; field != nil {
		*field = strings.TrimSpace(value)
	}
}

// sectionName is the name in a "[name]" header, which may be followed by a
// comment.
func sectionName(line string) (string, bool) {
	name, rest, ok := strings.Cut(line[1:], "]")
	if rest = strings.TrimSpace(rest); !ok || (rest != "" && rest[0] != '#' && rest[0] != ';') {
		return "", false
	}
	return strings.TrimSpace(name), true
}

// generatorTurnOff says how to stop a zram-generator file from defining its
// device: a package's drop-in under /usr is hidden by a link to /dev/null of
// its name in /etc; any other is the admin's, or made at runtime, to change.
func generatorTurnOff(file string) string {
	if dir, name := path.Split(file); path.Base(dir) == zramGeneratorDropInDir && strings.HasPrefix(file, "/usr/") {
		return fmt.Sprintf("to turn it off, link %s/%s to /dev/null", zramGeneratorEtcDropIns, name)
	}
	return "remove it, or take the device out of it"
}
