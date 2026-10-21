package btrfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Tree ids, item types and flags from btrfs_tree.h.
const (
	rootTreeObjectID    = 1
	fsTreeObjectID      = 5
	rootTreeDirObjectID = 6
	quotaTreeObjectID   = 8
	firstFreeObjectID   = 256
	lastFreeObjectID    = ^uint64(0) - 255 // BTRFS_LAST_FREE_OBJECTID, -256ULL

	dirItemKey      = 84
	rootItemKey     = 132
	rootBackrefKey  = 144
	qgroupStatusKey = 240
	qgroupInfoKey   = 242
	qgroupLimitKey  = 244

	rootSubvolRdonly   = 1 << 0 // BTRFS_ROOT_SUBVOL_RDONLY, in the root item
	subvolRdonly       = 1 << 1 // BTRFS_SUBVOL_RDONLY, in the subvolume ioctl flags
	qgroupStatusOn     = 1 << 0
	qgroupLimitMaxRfer = 1 << 0

	// Field offsets in struct btrfs_root_item; the uuid and time fields
	// exist only in the 439-byte v2 item.
	rootItemFlags      = 208
	rootItemUUID       = 247
	rootItemParentUUID = 263
	rootItemRecvUUID   = 279
	rootItemOtime      = 339
	rootItemV2Size     = 439
)

// searchKey mirrors struct btrfs_ioctl_search_key.
type searchKey struct {
	TreeID      uint64
	MinObjectID uint64
	MaxObjectID uint64
	MinOffset   uint64
	MaxOffset   uint64
	MinTransid  uint64
	MaxTransid  uint64
	MinType     uint32
	MaxType     uint32
	NrItems     uint32
	Unused      uint32
	Unused1     uint64
	Unused2     uint64
	Unused3     uint64
	Unused4     uint64
}

// searchHeader mirrors struct btrfs_ioctl_search_header; the item data
// follows each header in the buffer, unpadded.
type searchHeader struct {
	Transid  uint64
	ObjectID uint64
	Offset   uint64
	Type     uint32
	Len      uint32
}

const searchHeaderSize = 32

// searchArgs mirrors struct btrfs_ioctl_search_args_v2 with a 64 KiB
// buffer; the request number covers only the header.
type searchArgs struct {
	Key     searchKey
	BufSize uint64
	Buf     [64 << 10]byte
}

// inoLookupArgs mirrors struct btrfs_ioctl_ino_lookup_args.
type inoLookupArgs struct {
	TreeID   uint64
	ObjectID uint64
	Name     [4080]byte
}

// treeSearch calls fn for every item of the tree with a key between the
// search key's minimum and maximum, in key order. fn must not keep data.
func treeSearch(path string, key searchKey, fn func(searchHeader, []byte)) error {
	args := &searchArgs{Key: key}
	for {
		args.Key.NrItems = ^uint32(0)
		args.BufSize = uint64(len(args.Buf))
		if err := fsIoctl(path, iocTreeSearchV2, unsafe.Pointer(args)); err != nil {
			return err
		}
		if args.Key.NrItems == 0 {
			return nil
		}
		off := 0
		var last searchHeader
		for range args.Key.NrItems {
			if off+searchHeaderSize > len(args.Buf) {
				return errors.New("tree search: truncated item header")
			}
			b := args.Buf[off:]
			last = searchHeader{
				Transid: binary.LittleEndian.Uint64(b), ObjectID: binary.LittleEndian.Uint64(b[8:]), Offset: binary.LittleEndian.Uint64(b[16:]),
				Type: binary.LittleEndian.Uint32(b[24:]), Len: binary.LittleEndian.Uint32(b[28:]),
			}
			off += searchHeaderSize
			if off+int(last.Len) > len(args.Buf) {
				return errors.New("tree search: truncated item")
			}
			fn(last, args.Buf[off:off+int(last.Len)])
			off += int(last.Len)
		}
		// Continue just past the last key.
		args.Key.MinObjectID, args.Key.MinType, args.Key.MinOffset = last.ObjectID, last.Type, last.Offset
		switch {
		case args.Key.MinOffset < ^uint64(0):
			args.Key.MinOffset++
		case args.Key.MinType < 255:
			args.Key.MinType++
			args.Key.MinOffset = 0
		case args.Key.MinObjectID < ^uint64(0):
			args.Key.MinObjectID++
			args.Key.MinType, args.Key.MinOffset = 0, 0
		default:
			return nil
		}
	}
}

// inoLookup returns the path of directory inode objectid inside subvolume
// treeID, relative to that subvolume and without surrounding slashes.
func inoLookup(path string, treeID, objectid uint64) (string, error) {
	args := &inoLookupArgs{TreeID: treeID, ObjectID: objectid}
	if err := fsIoctl(path, iocInoLookup, unsafe.Pointer(args)); err != nil {
		return "", fmt.Errorf("resolve inode %d in subvolume %d: %w", objectid, treeID, err)
	}
	return strings.Trim(cstring(args.Name[:]), "/"), nil
}

// readSubvolumes lists the subvolumes and the default subvolume id of the
// filesystem mounted at top; a var so tests can serve rows.
var readSubvolumes = readSubvolumeTree

// rootBackref is where a subvolume hangs: its parent subvolume, the
// directory inode inside that parent, and its name there.
type rootBackref struct {
	parent, dirid uint64
	name          string
}

// readSubvolumeTree walks the root tree: every root item is a subvolume and
// its backref names the parent subvolume and directory, from which the full
// path is resolved. A root without a backref is being deleted and skipped.
func readSubvolumeTree(top string) ([]subvolumeRow, int, error) {
	rows, refs, err := readRootTree(top)
	if err != nil {
		return nil, 0, fmt.Errorf("list subvolumes: %w", err)
	}
	paths := map[uint64]string{fsTreeObjectID: ""}
	out := make([]subvolumeRow, 0, len(rows))
	for id, r := range rows {
		p, ok := subvolumePath(top, id, refs, paths)
		if !ok {
			continue
		}
		r.Path, r.ParentID = p, int(refs[id].parent)
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b subvolumeRow) int { return a.ID - b.ID })
	defaultID, err := readDefaultSubvolume(top)
	if err != nil {
		return nil, 0, err
	}
	return out, defaultID, nil
}

// readRootTree collects the root items and backrefs of every subvolume.
func readRootTree(top string) (map[uint64]subvolumeRow, map[uint64]rootBackref, error) {
	rows := map[uint64]subvolumeRow{}
	refs := map[uint64]rootBackref{}
	key := searchKey{
		TreeID: rootTreeObjectID, MinObjectID: firstFreeObjectID, MaxObjectID: lastFreeObjectID,
		MinType: rootItemKey, MaxType: rootBackrefKey, MaxOffset: ^uint64(0), MaxTransid: ^uint64(0),
	}
	err := treeSearch(top, key, func(h searchHeader, data []byte) {
		switch {
		case h.Type == rootItemKey && len(data) >= rootItemFlags+8:
			r := parseRootItem(data)
			r.ID = int(h.ObjectID)
			rows[h.ObjectID] = r
		case h.Type == rootBackrefKey && len(data) >= 18:
			nameLen := int(binary.LittleEndian.Uint16(data[16:]))
			if 18+nameLen <= len(data) {
				refs[h.ObjectID] = rootBackref{parent: h.Offset, dirid: binary.LittleEndian.Uint64(data), name: string(data[18 : 18+nameLen])}
			}
		}
	})
	return rows, refs, err
}

// subvolumePath resolves the path of subvolume id below the top level,
// memoizing in paths; ok is false for a root that is being deleted.
func subvolumePath(top string, id uint64, refs map[uint64]rootBackref, paths map[uint64]string) (string, bool) {
	if p, ok := paths[id]; ok {
		return p, true
	}
	ref, ok := refs[id]
	if !ok {
		return "", false
	}
	parent, ok := subvolumePath(top, ref.parent, refs, paths)
	if !ok {
		return "", false
	}
	dir, err := inoLookup(top, ref.parent, ref.dirid)
	if err != nil {
		slog.Warn("btrfs subvolume path", "id", id, "error", err)
		return "", false
	}
	var parts []string
	for _, s := range []string{parent, dir, ref.name} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	paths[id] = strings.Join(parts, "/")
	return paths[id], true
}

// parseRootItem reads the flags, uuids and creation time of a root item.
func parseRootItem(data []byte) subvolumeRow {
	r := subvolumeRow{ReadOnly: binary.LittleEndian.Uint64(data[rootItemFlags:])&rootSubvolRdonly != 0}
	if len(data) < rootItemV2Size {
		return r
	}
	r.UUID = uuidOrEmpty(data[rootItemUUID:])
	r.ParentUUID = uuidOrEmpty(data[rootItemParentUUID:])
	r.ReceivedUUID = uuidOrEmpty(data[rootItemRecvUUID:])
	if sec := binary.LittleEndian.Uint64(data[rootItemOtime:]); sec > 0 {
		t := time.Unix(int64(sec), int64(binary.LittleEndian.Uint32(data[rootItemOtime+8:])))
		r.CreatedAt = &t
	}
	return r
}

// uuidOrEmpty formats 16 bytes as a uuid, or "" when they are all zero.
func uuidOrEmpty(b []byte) string {
	if slices.ContainsFunc(b[:16], func(c byte) bool { return c != 0 }) {
		return formatUUID(b[:16])
	}
	return ""
}

// readDefaultSubvolume returns the id the "default" directory item of the
// root tree points at, or the top level when there is none.
func readDefaultSubvolume(top string) (int, error) {
	id := fsTreeObjectID
	key := searchKey{
		TreeID: rootTreeObjectID, MinObjectID: rootTreeDirObjectID, MaxObjectID: rootTreeDirObjectID,
		MinType: dirItemKey, MaxType: dirItemKey, MaxOffset: ^uint64(0), MaxTransid: ^uint64(0),
	}
	err := treeSearch(top, key, func(h searchHeader, data []byte) {
		// btrfs_dir_item: location key (objectid u64, type u8, offset u64),
		// transid u64, data_len u16, name_len u16, type u8, then the name.
		if len(data) < 30 {
			return
		}
		nameLen := int(binary.LittleEndian.Uint16(data[27:]))
		if 30+nameLen <= len(data) && string(data[30:30+nameLen]) == "default" {
			id = int(binary.LittleEndian.Uint64(data))
		}
	})
	if err != nil {
		return 0, fmt.Errorf("read default subvolume: %w", err)
	}
	return id, nil
}

// readQgroups returns the level-0 qgroup usage by subvolume id, or
// errQuotasDisabled; a var so tests can serve it.
var readQgroups = readQuotaTree

func readQuotaTree(top string) (map[int]QgroupUsage, error) {
	usage := map[int]QgroupUsage{}
	enabled := false
	key := searchKey{TreeID: quotaTreeObjectID, MinType: qgroupStatusKey, MaxType: qgroupLimitKey, MaxOffset: ^uint64(0), MaxTransid: ^uint64(0)}
	err := treeSearch(top, key, func(h searchHeader, data []byte) {
		// Qgroup ids are level << 48 | id; only level 0 maps to a subvolume.
		id := int(h.Offset & (1<<48 - 1))
		switch {
		case h.Type == qgroupStatusKey && len(data) >= 24:
			enabled = binary.LittleEndian.Uint64(data[16:])&qgroupStatusOn != 0
		case h.Offset>>48 != 0 || len(data) < 40:
		case h.Type == qgroupInfoKey:
			u := usage[id]
			u.Referenced, u.Exclusive = binary.LittleEndian.Uint64(data[8:]), binary.LittleEndian.Uint64(data[24:])
			usage[id] = u
		case h.Type == qgroupLimitKey && binary.LittleEndian.Uint64(data)&qgroupLimitMaxRfer != 0:
			u := usage[id]
			limit := binary.LittleEndian.Uint64(data[8:])
			u.MaxReferenced = &limit
			usage[id] = u
		}
	})
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil, errQuotasDisabled
	case err != nil:
		return nil, fmt.Errorf("read qgroups: %w", err)
	case !enabled:
		return nil, errQuotasDisabled
	}
	return usage, nil
}
