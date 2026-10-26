package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf16"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func hdNode(partNum uint32, sig [16]byte, mbrType, sigType byte) []byte {
	n := []byte{0x04, 0x01, 0x2a, 0x00}
	n = binary.LittleEndian.AppendUint32(n, partNum)
	n = binary.LittleEndian.AppendUint64(n, 2048)
	n = binary.LittleEndian.AppendUint64(n, 2000)
	n = append(n, sig[:]...)
	return append(n, mbrType, sigType)
}

var (
	// Real efibootmgr -v signatures, in on-disk (mixed-endian) order.
	sigUbuntu = [16]byte{0x29, 0xc3, 0x05, 0x3c, 0xcd, 0x5c, 0x35, 0x46, 0x93, 0x00, 0xb1, 0xae, 0xdb, 0x19, 0xaf, 0xaa}
	sigOther  = [16]byte{0x26, 0x1f, 0xf8, 0x57, 0xac, 0x82, 0x8c, 0x44, 0x89, 0x50, 0x56, 0x55, 0xd0, 0xdb, 0x9d, 0x91}
	endNode   = []byte{0x7f, 0xff, 0x04, 0x00}
)

func filePathNode() []byte {
	n := []byte{0x04, 0x04, 0, 0}
	for _, c := range utf16.Encode([]rune(`\EFI\ubuntu\shimx64.efi`)) {
		n = binary.LittleEndian.AppendUint16(n, c)
	}
	n = append(n, 0, 0)
	binary.LittleEndian.PutUint16(n[2:], uint16(len(n)))
	return n
}

func loadOption(attrs uint32, desc string, nodes ...[]byte) []byte {
	var path []byte
	for _, n := range nodes {
		path = append(path, n...)
	}
	b := binary.LittleEndian.AppendUint32(nil, attrs)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(path)))
	for _, c := range utf16.Encode([]rune(desc)) {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	b = append(b, 0, 0)
	return append(b, path...)
}

func writeEFIVar(t *testing.T, root, name string, data []byte) {
	t.Helper()
	body := append([]byte{7, 0, 0, 0}, data...)
	if err := os.WriteFile(filepath.Join(root, name+"-"+efiGlobalGU), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func u16s(v ...uint16) []byte {
	var b []byte
	for _, x := range v {
		b = binary.LittleEndian.AppendUint16(b, x)
	}
	return b
}

func TestReadBootEntries(t *testing.T) {
	root := t.TempDir()
	writeEFIVar(t, root, "BootOrder", u16s(0, 5, 6, 7))
	writeEFIVar(t, root, "BootCurrent", u16s(0))
	writeEFIVar(t, root, "Boot0000", loadOption(1, "Ubuntu", hdNode(1, sigUbuntu, 2, 2), filePathNode(), endNode))
	writeEFIVar(t, root, "Boot0005", loadOption(0, "Other", hdNode(2, sigOther, 2, 2), filePathNode(), endNode))
	// Not listed in BootOrder: sorts last.
	writeEFIVar(t, root, "Boot0001", loadOption(1, "Unlisted", hdNode(1, sigOther, 2, 2), endNode))
	// No hard drive node, MBR signature, and truncated data: all skipped.
	writeEFIVar(t, root, "Boot0006", loadOption(1, "Shell", filePathNode(), endNode))
	writeEFIVar(t, root, "Boot0007", loadOption(1, "MBR", hdNode(1, sigOther, 1, 1), endNode))
	writeEFIVar(t, root, "Boot0002", loadOption(1, "Cut", hdNode(1, sigOther, 2, 2), endNode)[:30])

	want := []apischema.EFIBootEntry{
		{ID: "0000", Label: "Ubuntu", PartUUID: "3c05c329-5ccd-4635-9300-b1aedb19afaa", Active: true, Current: true, Order: 0},
		{ID: "0005", Label: "Other", PartUUID: "57f81f26-82ac-448c-8950-5655d0db9d91", Order: 1},
		{ID: "0001", Label: "Unlisted", PartUUID: "57f81f26-82ac-448c-8950-5655d0db9d91", Active: true, Order: -1},
	}
	if got := readBootEntries(root); !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %+v\nwant     %+v", got, want)
	}
}

func TestReadBootEntriesMissingRoot(t *testing.T) {
	got := readBootEntries(filepath.Join(t.TempDir(), "absent"))
	if got == nil || len(got) != 0 {
		t.Errorf("got %#v, want empty non-nil slice", got)
	}
}

func TestParseLoadOptionGarbage(t *testing.T) {
	full := loadOption(1, "x", hdNode(1, sigUbuntu, 2, 2), endNode)
	for n := range len(full) {
		parseLoadOption(full[:n]) // must not panic on any prefix
	}
	if _, _, _, ok := parseLoadOption([]byte{1, 0, 0, 0, 9, 0, 0, 0, 0, 0, 4, 1, 0xff, 0xff}); ok {
		t.Error("garbage node length accepted")
	}
}
