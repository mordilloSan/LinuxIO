package storage

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	efiVarsRoot = "/sys/firmware/efi/efivars"
	efiGlobalGU = "8be4df61-93ca-11d2-aa0d-00e098032b8c"
)

// readBootEntries lists the firmware Boot#### entries under the efivarfs
// directory root that point at a GPT partition. Absent, unreadable or
// malformed variables are skipped: BIOS hosts and containers simply get none.
func readBootEntries(root string) []apischema.EFIBootEntry {
	order := map[string]int{}
	orderVar := readEFIVar(root, "BootOrder")
	for i := 0; i+2 <= len(orderVar); i += 2 {
		id := fmt.Sprintf("%04X", binary.LittleEndian.Uint16(orderVar[i:]))
		if _, dup := order[id]; !dup {
			order[id] = i / 2
		}
	}
	current := ""
	if v := readEFIVar(root, "BootCurrent"); len(v) >= 2 {
		current = fmt.Sprintf("%04X", binary.LittleEndian.Uint16(v))
	}
	files, _ := filepath.Glob(filepath.Join(root, "Boot[0-9A-F][0-9A-F][0-9A-F][0-9A-F]-"+efiGlobalGU))
	entries := []apischema.EFIBootEntry{}
	for _, file := range files {
		id := filepath.Base(file)[4:8]
		data := readEFIVar(root, "Boot"+id)
		active, label, partUUID, ok := parseLoadOption(data)
		if !ok {
			continue
		}
		pos, listed := order[id]
		if !listed {
			pos = -1
		}
		entries = append(entries, apischema.EFIBootEntry{
			ID: id, Label: label, PartUUID: partUUID,
			Active: active, Current: id == current, Order: pos,
		})
	}
	slices.SortFunc(entries, func(a, b apischema.EFIBootEntry) int {
		if (a.Order < 0) != (b.Order < 0) { // unlisted entries last
			return cmp.Compare(b.Order, a.Order)
		}
		return cmp.Or(cmp.Compare(a.Order, b.Order), strings.Compare(a.ID, b.ID))
	})
	return entries
}

// readEFIVar returns a variable's data without the 4-byte efivarfs attributes
// word, or nil when it cannot be read.
func readEFIVar(root, name string) []byte {
	data, err := os.ReadFile(filepath.Join(root, name+"-"+efiGlobalGU))
	if err != nil || len(data) < 4 {
		return nil
	}
	return data[4:]
}

// parseLoadOption decodes an EFI_LOAD_OPTION and reports the GPT partition
// GUID of its first Hard Drive device path node; ok is false without one.
func parseLoadOption(data []byte) (active bool, label, partUUID string, ok bool) {
	if len(data) < 6 {
		return false, "", "", false
	}
	active = binary.LittleEndian.Uint32(data)&1 != 0
	pathLen := int(binary.LittleEndian.Uint16(data[4:]))
	var desc []uint16
	rest := data[6:]
	for len(rest) >= 2 {
		c := binary.LittleEndian.Uint16(rest)
		rest = rest[2:]
		if c == 0 {
			break
		}
		desc = append(desc, c)
	}
	if pathLen > len(rest) {
		return false, "", "", false
	}
	path := rest[:pathLen]
	for len(path) >= 4 && path[0] != 0x7f {
		nodeLen := int(binary.LittleEndian.Uint16(path[2:]))
		if nodeLen < 4 || nodeLen > len(path) {
			return false, "", "", false
		}
		node := path[4:nodeLen]
		// Hard Drive media node: 4+8+8 bytes of partition numbers, then the
		// 16-byte signature, MBRType (2 = GPT) and SignatureType (2 = GUID).
		if path[0] == 4 && path[1] == 1 && len(node) == 38 && node[36] == 2 && node[37] == 2 {
			return active, string(utf16.Decode(desc)), formatEFIGUID(node[20:36]), true
		}
		path = path[nodeLen:]
	}
	return false, "", "", false
}

// formatEFIGUID prints a mixed-endian EFI GUID as the lowercase 8-4-4-4-12
// string that matches a partition's PARTUUID.
func formatEFIGUID(b []byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%x-%x",
		binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint16(b[4:]),
		binary.LittleEndian.Uint16(b[6:]), b[8:10], b[10:16])
}
