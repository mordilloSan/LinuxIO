package btrfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// treeItem is one item of a fake btrfs tree.
type treeItem struct {
	tree, objectid, offset uint64
	typ                    uint32
	data                   []byte
}

// fakeTrees answers tree searches from items, inode lookups from dirs
// (keyed "tree/objectid") and records subvolume flag changes.
type fakeTrees struct {
	items   []treeItem
	dirs    map[string]string
	missing map[uint64]bool // trees that do not exist
	flags   map[string]uint64
}

func keyLess(a, b [3]uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func (f *fakeTrees) ioctl(path string, req uintptr, arg unsafe.Pointer) error {
	switch req {
	case iocTreeSearchV2:
		a := (*searchArgs)(arg)
		if f.missing[a.Key.TreeID] {
			return unix.ENOENT
		}
		lo := [3]uint64{a.Key.MinObjectID, uint64(a.Key.MinType), a.Key.MinOffset}
		hi := [3]uint64{a.Key.MaxObjectID, uint64(a.Key.MaxType), a.Key.MaxOffset}
		off, n := 0, uint32(0)
		for _, it := range f.items {
			k := [3]uint64{it.objectid, uint64(it.typ), it.offset}
			if it.tree != a.Key.TreeID || keyLess(k, lo) || keyLess(hi, k) {
				continue
			}
			if off+32+len(it.data) > len(a.Buf) {
				break // the kernel stops at a full buffer
			}
			b := a.Buf[off:]
			binary.LittleEndian.PutUint64(b[8:], it.objectid)
			binary.LittleEndian.PutUint64(b[16:], it.offset)
			binary.LittleEndian.PutUint32(b[24:], it.typ)
			binary.LittleEndian.PutUint32(b[28:], uint32(len(it.data)))
			copy(b[32:], it.data)
			off += 32 + len(it.data)
			n++
		}
		a.Key.NrItems = n
	case iocInoLookup:
		a := (*inoLookupArgs)(arg)
		dir, ok := f.dirs[fmt.Sprintf("%d/%d", a.TreeID, a.ObjectID)]
		if !ok {
			return unix.ENOENT
		}
		a.Name = [4080]byte{}
		copy(a.Name[:], dir)
	case iocSubvolGetFlags:
		*(*uint64)(arg) = f.flags[path]
	case iocSubvolSetFlags:
		f.flags[path] = *(*uint64)(arg)
	default:
		return fmt.Errorf("unexpected ioctl %#x", req)
	}
	return nil
}

func stubTrees(t *testing.T, f *fakeTrees) {
	t.Helper()
	old := fsIoctl
	fsIoctl = f.ioctl
	t.Cleanup(func() { fsIoctl = old })
}

func rootItem(flags uint64, uuid, parentUUID byte, otime int64) []byte {
	b := make([]byte, rootItemV2Size)
	binary.LittleEndian.PutUint64(b[rootItemFlags:], flags)
	b[rootItemUUID] = uuid
	b[rootItemParentUUID] = parentUUID
	binary.LittleEndian.PutUint64(b[rootItemOtime:], uint64(otime))
	return b
}

func backref(dirid uint64, name string) []byte {
	b := make([]byte, 18+len(name))
	binary.LittleEndian.PutUint64(b, dirid)
	binary.LittleEndian.PutUint16(b[16:], uint16(len(name)))
	copy(b[18:], name)
	return b
}

func dirItem(target uint64, name string) []byte {
	b := make([]byte, 30+len(name))
	binary.LittleEndian.PutUint64(b, target)
	binary.LittleEndian.PutUint16(b[27:], uint16(len(name)))
	copy(b[30:], name)
	return b
}

func TestReadSubvolumeTreeResolvesPathsFlagsAndTimes(t *testing.T) {
	created := time.Date(2026, 10, 19, 10, 15, 0, 0, time.UTC)
	f := &fakeTrees{
		items: []treeItem{
			// @ and @home under the top level; a nested subvolume inside @;
			// a read-only snapshot of @; an orphan root being deleted.
			{rootTreeObjectID, 256, 0, rootItemKey, rootItem(0, 1, 0, 0)},
			{rootTreeObjectID, 256, 5, rootBackrefKey, backref(256, "@")},
			{rootTreeObjectID, 257, 0, rootItemKey, rootItem(0, 2, 0, 0)},
			{rootTreeObjectID, 257, 5, rootBackrefKey, backref(256, "@home")},
			{rootTreeObjectID, 300, 0, rootItemKey, rootItem(0, 3, 0, 0)},
			{rootTreeObjectID, 300, 256, rootBackrefKey, backref(777, "abc")},
			{rootTreeObjectID, 301, 12100, rootItemKey, rootItem(rootSubvolRdonly, 4, 1, created.Unix())},
			{rootTreeObjectID, 301, 5, rootBackrefKey, backref(888, "@-2026-10-19")},
			{rootTreeObjectID, 302, 0, rootItemKey, rootItem(0, 5, 0, 0)},
			// Old 239-byte root item without uuids survives too.
			{rootTreeObjectID, 303, 0, rootItemKey, rootItem(0, 6, 0, 0)[:239]},
			{rootTreeObjectID, 303, 5, rootBackrefKey, backref(256, "old")},
			// Other item types in the key range are ignored.
			{rootTreeObjectID, 257, 5, 156, backref(256, "@home")},
			{rootTreeObjectID, rootTreeDirObjectID, 12345, dirItemKey, dirItem(257, "default")},
		},
		dirs: map[string]string{"5/256": "", "256/777": "var/lib/docker/btrfs/subvolumes/", "5/888": "linuxio-snapshots/"},
	}
	stubTrees(t, f)

	rows, defaultID, err := readSubvolumeTree("/mnt/top")
	if err != nil {
		t.Fatal(err)
	}
	if defaultID != 257 {
		t.Fatalf("default = %d, want 257", defaultID)
	}
	uuid := func(n byte) string { return formatUUID(append([]byte{n}, make([]byte, 15)...)) }
	want := []subvolumeRow{
		{ID: 256, ParentID: 5, Path: "@", UUID: uuid(1)},
		{ID: 257, ParentID: 5, Path: "@home", UUID: uuid(2)},
		{ID: 300, ParentID: 256, Path: "@/var/lib/docker/btrfs/subvolumes/abc", UUID: uuid(3)},
		{ID: 301, ParentID: 5, Path: "linuxio-snapshots/@-2026-10-19", UUID: uuid(4), ParentUUID: uuid(1), ReadOnly: true, CreatedAt: &created},
		{ID: 303, ParentID: 5, Path: "old"},
	}
	for i := range rows {
		if rows[i].CreatedAt != nil {
			utc := rows[i].CreatedAt.UTC()
			rows[i].CreatedAt = &utc
		}
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows =\n%+v\nwant\n%+v", rows, want)
	}
}

func TestReadSubvolumeTreeDefaultsToTopLevel(t *testing.T) {
	stubTrees(t, &fakeTrees{dirs: map[string]string{}})
	rows, defaultID, err := readSubvolumeTree("/mnt/top")
	if err != nil || len(rows) != 0 || defaultID != fsTreeObjectID {
		t.Fatalf("rows=%v default=%d err=%v", rows, defaultID, err)
	}
}

func qgroupInfo(rfer, excl uint64) []byte {
	b := make([]byte, 40)
	binary.LittleEndian.PutUint64(b[8:], rfer)
	binary.LittleEndian.PutUint64(b[24:], excl)
	return b
}

func qgroupLimitItem(flags, maxRfer uint64) []byte {
	b := make([]byte, 40)
	binary.LittleEndian.PutUint64(b, flags)
	binary.LittleEndian.PutUint64(b[8:], maxRfer)
	return b
}

func TestReadQuotaTree(t *testing.T) {
	status := make([]byte, 32)
	binary.LittleEndian.PutUint64(status[16:], qgroupStatusOn)
	f := &fakeTrees{items: []treeItem{
		{quotaTreeObjectID, 0, 0, qgroupStatusKey, status},
		{quotaTreeObjectID, 0, 5, qgroupInfoKey, qgroupInfo(16384, 16384)},
		{quotaTreeObjectID, 0, 256, qgroupInfoKey, qgroupInfo(1234567890, 12345678)},
		{quotaTreeObjectID, 0, 256, qgroupLimitKey, qgroupLimitItem(qgroupLimitMaxRfer, 2000000000)},
		{quotaTreeObjectID, 0, 257, qgroupInfoKey, qgroupInfo(4194304, 4194304)},
		{quotaTreeObjectID, 0, 257, qgroupLimitKey, qgroupLimitItem(0, 99)},
		{quotaTreeObjectID, 0, 1<<48 | 100, qgroupInfoKey, qgroupInfo(1, 1)},
	}}
	stubTrees(t, f)
	got, err := readQuotaTree("/mnt/top")
	if err != nil {
		t.Fatal(err)
	}
	limit := uint64(2000000000)
	want := map[int]QgroupUsage{
		5:   {Referenced: 16384, Exclusive: 16384},
		256: {Referenced: 1234567890, Exclusive: 12345678, MaxReferenced: &limit},
		257: {Referenced: 4194304, Exclusive: 4194304},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	binary.LittleEndian.PutUint64(status[16:], 0)
	if _, err := readQuotaTree("/mnt/top"); !errors.Is(err, errQuotasDisabled) {
		t.Fatalf("status off: err = %v", err)
	}
	stubTrees(t, &fakeTrees{missing: map[uint64]bool{quotaTreeObjectID: true}})
	if _, err := readQuotaTree("/mnt/top"); !errors.Is(err, errQuotasDisabled) {
		t.Fatalf("no quota tree: err = %v", err)
	}
}

func TestSetSubvolumeReadOnlyKeepsOtherFlags(t *testing.T) {
	f := &fakeTrees{flags: map[string]uint64{"/mnt/top/@": 1 << 4}}
	stubTrees(t, f)
	if err := setSubvolumeReadOnly("/mnt/top/@", true); err != nil || f.flags["/mnt/top/@"] != 1<<4|subvolRdonly {
		t.Fatalf("set: flags=%#x err=%v", f.flags["/mnt/top/@"], err)
	}
	if err := setSubvolumeReadOnly("/mnt/top/@", false); err != nil || f.flags["/mnt/top/@"] != 1<<4 {
		t.Fatalf("clear: flags=%#x err=%v", f.flags["/mnt/top/@"], err)
	}
}

func TestTreeSearchContinuesPastFullBuffer(t *testing.T) {
	// Three items of 40 KiB do not fit one 64 KiB buffer, so the search
	// must resume after the last key it was handed.
	var items []treeItem
	for i := uint64(1); i <= 3; i++ {
		items = append(items, treeItem{rootTreeObjectID, i, 0, rootItemKey, make([]byte, 40<<10)})
	}
	f := &fakeTrees{items: items}
	stubTrees(t, f)
	var seen []uint64
	err := treeSearch("/x", searchKey{TreeID: rootTreeObjectID, MaxObjectID: ^uint64(0), MaxType: 255, MaxOffset: ^uint64(0)}, func(h searchHeader, data []byte) {
		seen = append(seen, h.ObjectID)
		if len(data) != 40<<10 {
			t.Errorf("item %d: len = %d", h.ObjectID, len(data))
		}
	})
	if err != nil || !reflect.DeepEqual(seen, []uint64{1, 2, 3}) {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
}

// opsKernel records the subvolume ioctls with their decoded arguments.
type opsKernel struct{ calls []string }

func (k *opsKernel) ioctl(path string, req uintptr, arg unsafe.Pointer) error {
	switch req {
	case iocSubvolCreate:
		k.calls = append(k.calls, "create "+path+" "+cstring((*volArgs)(arg).Name[:]))
	case iocSnapDestroy:
		k.calls = append(k.calls, "destroy "+path+" "+cstring((*volArgs)(arg).Name[:]))
	case iocSnapCreateV2:
		a := (*volArgsV2)(arg)
		k.calls = append(k.calls, fmt.Sprintf("snapshot %s %s flags=%d fd=%v", path, cstring(a.Name[:]), a.Flags, a.Fd > 0))
	case iocDefaultSubvol:
		k.calls = append(k.calls, fmt.Sprintf("default %s %d", path, *(*uint64)(arg)))
	case iocQuotaCtl:
		k.calls = append(k.calls, fmt.Sprintf("quota %s %d", path, (*quotaCtlArgs)(arg).Cmd))
	case iocQgroupLimit:
		a := (*qgroupLimitArgs)(arg)
		k.calls = append(k.calls, fmt.Sprintf("limit %s id=%d flags=%d max=%d", path, a.QgroupID, a.Flags, a.MaxRfer))
	case iocInoLookup:
		(*inoLookupArgs)(arg).TreeID = 301
	case iocSend:
		a := (*sendArgs)(arg)
		k.calls = append(k.calls, fmt.Sprintf("send %s parent=%d clones=%d set=%v", path, a.ParentRoot, a.CloneSourcesCount, a.CloneSources != 0))
	default:
		return fmt.Errorf("unexpected ioctl %#x", req)
	}
	return nil
}

func TestSubvolumeOpsIssueIoctls(t *testing.T) {
	k := &opsKernel{}
	old := fsIoctl
	fsIoctl = k.ioctl
	t.Cleanup(func() { fsIoctl = old })
	src := t.TempDir()
	limit := uint64(5000)
	for name, err := range map[string]error{
		"create":   createSubvolumeAt("/mnt/top/data"),
		"destroy":  destroySubvolume("/mnt/top/linuxio-snapshots/@/x"),
		"snapshot": snapshotSubvolumeTo(src, "/mnt/top/linuxio-snapshots/@/y", true),
		"default":  setDefaultSubvolumeID("/mnt/top", 257),
		"quota":    quotaCtl("/mnt/top", true),
		"limit":    qgroupLimit("/mnt/top/@", &limit),
		"unlimit":  qgroupLimit("/mnt/top/@", nil),
	} {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	slices.Sort(k.calls)
	want := []string{
		"create /mnt/top data",
		"default /mnt/top 257",
		"destroy /mnt/top/linuxio-snapshots/@ x",
		"limit /mnt/top/@ id=0 flags=1 max=18446744073709551615",
		"limit /mnt/top/@ id=0 flags=1 max=5000",
		"quota /mnt/top 1",
		"snapshot /mnt/top/linuxio-snapshots/@ y flags=2 fd=true",
	}
	if !reflect.DeepEqual(k.calls, want) {
		t.Fatalf("calls =\n%q\nwant\n%q", k.calls, want)
	}
	if err := createSubvolumeAt("/mnt/top/" + strings.Repeat("x", 256)); err == nil {
		t.Fatal("over-long name accepted")
	}
}

func TestSendStreamPassesParentAsCloneSource(t *testing.T) {
	k := &opsKernel{}
	old := fsIoctl
	fsIoctl = k.ioctl
	t.Cleanup(func() { fsIoctl = old })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if err := sendStream("/top/snap", "/top/parent", w); err != nil {
		t.Fatal(err)
	}
	if err := sendStream("/top/snap", "", w); err != nil {
		t.Fatal(err)
	}
	if want := []string{"send /top/snap parent=301 clones=1 set=true", "send /top/snap parent=0 clones=0 set=false"}; !reflect.DeepEqual(k.calls, want) {
		t.Fatalf("calls = %q, want %q", k.calls, want)
	}
}
