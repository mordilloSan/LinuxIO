package btrfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// claimDisk is the loop disk TestPartitionClaimIntegration works on, with
// its two partitions and libblkid's command to compare signatures with.
type claimDisk struct {
	*itDisk
	p1, p2, blkid string
}

// claimParts are the partitions of a claimDisk, as the kernel lists them.
var claimParts = []kernelPartition{{1, 1 << 20, 128 << 20}, {2, 256 << 20, 128 << 20}}

// claim holds device exclusively until the release, as a mount, a swapon,
// an md array or an LVM volume made after a caller's check would.
func (d *claimDisk) claim(device string) (release func()) {
	d.t.Helper()
	f, err := os.OpenFile(device, os.O_RDONLY|os.O_EXCL, 0)
	d.must("claim "+device, err)
	return func() { f.Close() }
}

// exclusiveOpenErr is what an exclusive open of device returns, which the
// kernel refuses while something claims it; a granted open is closed again.
func exclusiveOpenErr(device string) error {
	f, err := os.OpenFile(device, os.O_RDONLY|os.O_EXCL, 0)
	if err == nil {
		f.Close()
	}
	return err
}

func (d *claimDisk) readAt(device string, off int64, n int) []byte {
	d.t.Helper()
	f, err := os.Open(device)
	d.must("open "+device, err)
	defer f.Close()
	b := make([]byte, n)
	_, err = f.ReadAt(b, off)
	d.must("read "+device, err)
	return b
}

// signature reads part's signature, failing unless it is kind and what
// libblkid reads there.
func (d *claimDisk) signature(part, kind string) MemberSignature {
	d.t.Helper()
	sig, err := ReadMemberSignature(part)
	d.must("read the signature of "+part, err)
	if k, u := blkidMember(d.t, d.blkid, part); sig.Kind != kind || k != kind || u != sig.UUID {
		d.t.Fatalf("%s: read %+v, blkid reads %q %q; want %s", part, sig, k, u, kind)
	}
	return sig
}

// TestPartitionClaimIntegration checks against the real kernel that every
// writer refuses a device claimed after its caller checked it, with nothing
// written, that Initialize keeps the disk claimed from the signature check
// to the table write, and that the md and LVM2 signatures read from a device
// are the ones libblkid reads, also those mdadm and pvcreate write when they
// are installed. Every step holds the disk's udev lock, so udev probes
// nothing the test writes. Run it with `make test-btrfs-integration`.
func TestPartitionClaimIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the partition integration test needs root")
	}
	blkid, err := exec.LookPath("blkid")
	if err != nil {
		t.Fatal("the integration test compares signatures with blkid, from util-linux")
	}
	// 2 GiB, sparse: room for a pool partition and its slack, so the
	// PartitionDisk refusal below is about the partitions.
	dir := t.TempDir()
	d := &claimDisk{itDisk: &itDisk{t: t, ctx: context.Background(), dev: loopDevice(t, filepath.Join(dir, "disk.img"), 2<<30)}, blkid: blkid}
	d.locked(func() {
		d.must("initialize", InitializeDisk(d.ctx, d.dev, nil))
		d.p1 = d.create(claimParts[0])
		d.p2 = d.create(claimParts[1])
	})
	d.locked(d.claimedRefused)
	d.locked(d.signaturesUnderClaim)
	d.locked(d.toolSignatures)
}

// claimedRefused checks that each writer refuses a device claimed since its
// caller checked it, and writes nothing.
func (d *claimDisk) claimedRefused() {
	t := d.t
	fsid := [16]byte{0xc1, 0xa1, 0x31}
	writeAt(t, d.p1, superOffset, superblock(fsid, "it", 1, 1, 128<<20))
	before := d.readAt(d.p1, superOffset, superSize)
	release := d.claim(d.p1)
	d.busy("wipe of a claimed pool device", WipeDevice(d.p1))
	_, err := WipePoolSuperblocks(d.p1, formatUUID(fsid[:]))
	d.busy("undo of a pool create on a claimed device", err)
	_, err = ReadMemberSignature(d.p1)
	d.busy("signature read of a claimed device", err)
	d.busy("forced add's wipe of a claimed device", wipeDeviceEdges(d.p1))
	err = discardDevice(d.ctx, d.p1, 128<<20, func(uint64) {})
	if d.busy("trim of a claimed device", err); !strings.Contains(err.Error(), d.p1+" is in use") {
		t.Fatalf("trim of a claimed device = %v, want it to name %s in use", err, d.p1)
	}
	if !bytes.Equal(d.readAt(d.p1, superOffset, superSize), before) {
		t.Fatal("a refused writer changed the claimed device")
	}
	release()
	if wiped, wipeErr := WipePoolSuperblocks(d.p1, formatUUID(fsid[:])); wipeErr != nil || !wiped {
		t.Fatalf("undo once released: wiped=%t err=%v", wiped, wipeErr)
	}

	gpt := d.gptBytes()
	// A disk with partitions, which a pool partitions only when it has
	// none, is refused before anything is written.
	if _, err = PartitionDisk(d.ctx, d.dev); !errors.Is(err, ErrHasPartitions) {
		t.Fatalf("partitioning a disk with partitions = %v, want ErrHasPartitions", err)
	}
	release = d.claim(d.dev)
	d.busy("initializing a claimed disk", InitializeDisk(d.ctx, d.dev, nil))
	release()
	// A claimed partition is a claim on its disk too (bd_may_claim).
	release = d.claim(d.p2)
	d.busy("initializing a disk with a claimed partition", InitializeDisk(d.ctx, d.dev, nil))
	release()
	if !bytes.Equal(d.gptBytes(), gpt) {
		t.Fatal("a refused table write changed the partition table")
	}
	d.table(claimParts...)
}

// signaturesUnderClaim checks that an md superblock and an LVM2 label no
// udev probe has seen are read as libblkid reads them, one by one and by
// Initialize under its claim, which no other exclusive open of the disk or
// a partition gets past until the table is written.
func (d *claimDisk) signaturesUnderClaim() {
	t := d.t
	writeAt(t, d.p1, 0x1000, md1Superblock(0x1000, [16]byte{0xa1, 0x2b, 0x3c}, 0))
	writeAt(t, d.p2, 512, lvm2Label(1, "LinuxIOclaimIntegrationTestPV001"))
	read := []MemberSignature{d.signature(d.p1, "md"), d.signature(d.p2, "lvm")}
	refusal := errors.New("not confirmed")
	var found []MemberSignature
	gpt := d.gptBytes()
	err := InitializeDisk(d.ctx, d.dev, func(f []MemberSignature) error {
		found = f
		for _, dev := range []string{d.dev, d.p1, d.p2} {
			d.busy("exclusive open of "+dev+" while Initialize holds the disk", exclusiveOpenErr(dev))
		}
		return refusal
	})
	if !errors.Is(err, refusal) || !slices.Equal(found, read) {
		t.Fatalf("initialize: err = %v, found %+v; want the refusal after %+v", err, found, read)
	}
	if !bytes.Equal(d.gptBytes(), gpt) {
		t.Fatal("the refused initialize changed the partition table")
	}
	d.table(claimParts...)
	writeAt(t, d.p1, 0x1000, make([]byte, 4096))
	writeAt(t, d.p2, 512, make([]byte, 512))
}

// toolSignatures checks the signatures mdadm and pvcreate write, when they
// are installed.
func (d *claimDisk) toolSignatures() {
	t := d.t
	if mdadm, err := exec.LookPath("mdadm"); err == nil {
		const array = "/dev/md/linuxio-claim-it"
		for _, metadata := range []string{"1.2", "1.0", "0.90"} {
			runTool(t, mdadm, "--create", array, "--run", "--force", "--level=1", "--raid-devices=1", "--metadata="+metadata, d.p1)
			runTool(t, mdadm, "--stop", array)
			d.signature(d.p1, "md")
			runTool(t, mdadm, "--zero-superblock", d.p1)
		}
	} else {
		t.Log("mdadm not installed; skipping its superblocks")
	}
	if pvcreate, err := exec.LookPath("pvcreate"); err == nil {
		runTool(t, pvcreate, "-ff", "-y", d.p2)
		d.signature(d.p2, "lvm")
		runTool(t, "pvremove", "-ff", "-y", d.p2)
	} else {
		t.Log("pvcreate not installed; skipping its label")
	}
}

// runTool runs a tool the test drives, failing with its output.
func runTool(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
