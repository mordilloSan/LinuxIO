package btrfs

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseCompressOptionBareForms(t *testing.T) {
	for opt, want := range map[string]struct {
		alg   string
		force bool
	}{"compress": {"zlib", false}, "compress-force": {"zlib", true}, "compress=no": {"none", false}} {
		alg, level, force, ok := ParseCompressOption(opt)
		if !ok || alg != want.alg || force != want.force || level != nil {
			t.Errorf("ParseCompressOption(%q) = %q, %v, %v, %v", opt, alg, level, force, ok)
		}
	}
	if _, _, _, ok := ParseCompressOption("compression=zstd"); ok {
		t.Error("compression=zstd must not parse")
	}
}

func TestSetCompressionRejectsAlgorithm(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256}})
	calls := stubMountFS(t, nil)
	ints := func(n int) *int { return &n }
	for _, tc := range []struct {
		alg   string
		level *int
		want  string
	}{
		{"brotli", nil, "unsupported compression algorithm brotli"},
		{"zstd", ints(16), "level"},
		{"zstd", ints(0), "level"},
		{"zlib", ints(10), "level"},
		{"lzo", ints(1), "level"},
		{"none", ints(1), "level"},
	} {
		_, err := SetCompression(context.Background(), rootUUID, tc.alg, tc.level, false)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SetCompression(%s, %v) err = %v, want %q", tc.alg, tc.level, err, tc.want)
		}
	}
	if _, err := SetCompression(context.Background(), dataUUID, "zstd", nil, false); err == nil {
		t.Error("expected error for a filesystem without a user mount")
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected mounts: %v", *calls)
	}
}

func TestSetCompressionRemounts(t *testing.T) {
	stubMounts(t, []Mount{
		{Mountpoint: filepath.Join(TopLevelDir, rootUUID), UUID: rootUUID, SubvolID: 5},
		{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256, Options: []string{"rw", "noatime", "subvol=/@"}},
		{Mountpoint: "/home", UUID: rootUUID, Subvol: "@home", SubvolID: 257},
	})
	var calls []string
	var flags []uintptr
	old := mountFS
	mountFS = func(source, target, fstype string, f uintptr, data string) error {
		calls = append(calls, source+" "+target+" "+fstype+" "+data)
		flags = append(flags, f)
		return nil
	}
	t.Cleanup(func() { mountFS = old })
	three := 3
	for _, tc := range []struct {
		alg    string
		level  *int
		force  bool
		option string
	}{
		{"zstd", &three, false, "compress=zstd:3"},
		{"zstd", nil, true, "compress-force=zstd"},
		{"lzo", nil, false, "compress=lzo"},
		{"none", nil, true, "compress=no"},
	} {
		calls, flags = nil, nil
		opt, err := SetCompression(context.Background(), rootUUID, tc.alg, tc.level, tc.force)
		if err != nil || opt != tc.option {
			t.Fatalf("SetCompression(%s) = %q, %v; want %q", tc.alg, opt, err, tc.option)
		}
		// The remount repeats the mount's own flags and sends only the new option.
		if len(calls) != 1 || calls[0] != " /  "+tc.option || flags[0] != unix.MS_REMOUNT|unix.MS_NOATIME {
			t.Fatalf("calls = %v flags = %#x", calls, flags)
		}
	}
}

func TestGetSettingsParsesCompression(t *testing.T) {
	stubMounts(t, []Mount{
		{Mountpoint: filepath.Join(TopLevelDir, rootUUID), UUID: rootUUID, SubvolID: 5, Options: []string{"rw", "compress-force=zstd:5"}},
		{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256, Options: []string{"rw", "compress=zstd:3"}},
		{Mountpoint: "/home", UUID: rootUUID, Subvol: "@home", SubvolID: 257, Options: []string{"rw"}},
		{Mountpoint: "/other", UUID: dataUUID, SubvolID: 5, Options: []string{"compress=zlib:9"}},
	})
	stubQgroups(t, nil, errQuotasDisabled)

	got, err := GetSettings(context.Background(), rootUUID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	three := 3
	want := BtrfsSettings{UUID: rootUUID, Compression: BtrfsCompression{Algorithm: "zstd", Level: &three}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	stubQgroups(t, map[int]QgroupUsage{}, nil)
	if got, err = GetSettings(context.Background(), rootUUID); err != nil || !got.QuotasEnabled {
		t.Fatalf("quotas on: %+v, %v", got, err)
	}
	stubQgroups(t, nil, errors.New("read qgroups: boom"))
	if _, err = GetSettings(context.Background(), rootUUID); err == nil {
		t.Fatal("expected qgroup error to propagate")
	}
}

func TestQuotaAndPropertyCommands(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/mnt/top", UUID: rootUUID, SubvolID: 5}})
	calls := stubSubvolumeBtrfs(t, 256)
	limit2G := uint64(2000000000)
	stubQgroups(t, map[int]QgroupUsage{256: {Referenced: 1234567890, Exclusive: 12345678, MaxReferenced: &limit2G}}, nil)
	ctx := context.Background()

	if err := SetQuotas(ctx, rootUUID, true); err != nil {
		t.Fatal(err)
	}
	if err := SetQuotas(ctx, rootUUID, false); err != nil {
		t.Fatal(err)
	}
	limit := uint64(5000)
	if _, err := SetQgroupLimit(ctx, rootUUID, "@", &limit); err != nil {
		t.Fatal(err)
	}
	zero := uint64(0)
	// Checked before the subvolume lookup: even an unknown path gets it.
	if _, err := SetQgroupLimit(ctx, rootUUID, "missing", &zero); err == nil || !strings.Contains(err.Error(), "greater than zero") {
		t.Fatalf("zero limit err = %v, want a greater-than-zero error", err)
	}
	sv, err := SetQgroupLimit(ctx, rootUUID, "@", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sv.MaxReferencedBytes == nil || *sv.MaxReferencedBytes != 2000000000 {
		t.Fatalf("limit not populated: %+v", sv.MaxReferencedBytes)
	}
	if sv.ReferencedBytes == nil || *sv.ReferencedBytes != 1234567890 || sv.ExclusiveBytes == nil || *sv.ExclusiveBytes != 12345678 {
		t.Fatalf("sizes not populated: %+v", sv)
	}
	if _, err := SetSubvolumeCompression(ctx, rootUUID, "@", "zstd"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSubvolumeCompression(ctx, rootUUID, "@", "none"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSubvolumeCompression(ctx, rootUUID, "@", "brotli"); err == nil {
		t.Fatal("expected unsupported algorithm error")
	}
	want := [][]string{
		{"quota", "enable", "/mnt/top"},
		{"quota", "disable", "/mnt/top"},
		{"qgroup", "limit", "5000", "/mnt/top/@"},
		{"qgroup", "limit", "none", "/mnt/top/@"},
		{"xattr", "/mnt/top/@", "btrfs.compression", "zstd"},
		{"xattr", "/mnt/top/@", "btrfs.compression", ""},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %q, want %q", *calls, want)
	}
}
