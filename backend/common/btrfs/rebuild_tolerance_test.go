package btrfs

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// memberDisk is the hardware identity of the disk of device id in the pools
// these tests decide on.
func memberDisk(id int) BtrfsSpareDisk {
	return BtrfsSpareDisk{Serial: "S" + string(rune('0'+id)), SizeBytes: 1000}
}

// threeDiskInput is what decideRebuild decides on for a pool on three disks,
// every kind of chunk in profile, with the devices in missing gone. The disk
// of the last of them is back as /dev/sdx, carrying its superblock, and the
// health run before this one saw it.
func threeDiskInput(profile string, missing ...int) rebuildInput {
	in := rebuildInput{
		fs:         BtrfsFilesystem{UUID: dataUUID, HealthKnown: true, Mountpoints: []Mount{{Mountpoint: "/data", UUID: dataUUID}}},
		generation: 20, boot: "boot", now: time.Now(),
	}
	for _, kind := range []string{"data", "metadata", "system"} {
		in.fs.Allocation = append(in.fs.Allocation, BtrfsAllocation{Kind: kind, Profile: profile})
	}
	for id := 1; id <= 3; id++ {
		d := BtrfsDevice{ID: id, UUID: devUUID(id), SizeBytes: 1000, Path: "/dev/sd" + string(rune('a'+id)), Missing: slices.Contains(missing, id)}
		in.fs.Devices = append(in.fs.Devices, d)
		in.state.Identities = append(in.state.Identities, memberIdentity{ID: uint64(id), UUID: d.UUID, Disk: memberDisk(id)})
		if d.Missing {
			in.state.Missing = append(in.state.Missing, missingDisk{ID: uint64(id), UUID: d.UUID, At: in.now.Add(-time.Hour).Unix()})
		} else {
			in.disks = append(in.disks, spareCandidate{device: d.Path, disk: memberDisk(id)})
		}
	}
	back := missing[len(missing)-1]
	in.disks = append(in.disks, spareCandidate{device: "/dev/sdx", disk: memberDisk(back)})
	in.devices = []poolDevice{{
		device: "/dev/sdx", sb: Superblock{UUID: dataUUID, DevID: uint64(back), DevUUID: devUUID(back), Generation: 10},
		disk: memberDisk(back), whole: true, probed: true, seq: 7,
	}}
	in.state.Returned = &returnedSighting{ID: uint64(back), Disk: memberDisk(back), Device: "/dev/sdx", Seen: 1, Boot: "boot", Seq: 7}
	return in
}

// With two members of a raid1 on three disks missing, the chunks those two
// held together have no copy on the disk that stayed: erasing the one that
// came back would erase their only copy, and a replace onto it, or onto the
// spare, could not copy them. Nothing is rebuilt until the pool is back
// within what its profiles lose.
func TestRebuildWaitsForThePoolToBeWithinItsTolerance(t *testing.T) {
	t.Run("one member missing rebuilds onto its disk", func(t *testing.T) {
		if p := decideRebuild(threeDiskInput("raid1", 3)); p.kind != rebuildReturned || p.target != "/dev/sdx" {
			t.Fatalf("plan = %+v, want the rebuild onto /dev/sdx", p)
		}
	})
	t.Run("two members of raid1 missing, one back", func(t *testing.T) {
		p := decideRebuild(threeDiskInput("raid1", 2, 3))
		if p.kind != rebuildRefuse || p.refused != nil {
			t.Fatalf("plan = %+v, want a refusal that names no disk for the episode", p)
		}
		if !strings.Contains(p.reason, "devices 2 and 3 are missing") || !strings.Contains(p.reason, "raid1") {
			t.Errorf("reason = %q, want the missing devices and the profile", p.reason)
		}
		if !strings.Contains(p.advice, "so btrfs takes them back") || strings.Contains(strings.ToLower(p.advice), "replace") {
			t.Errorf("advice = %q, want the remount and no replace", p.advice)
		}
	})
	t.Run("raid1c3 loses two", func(t *testing.T) {
		if p := decideRebuild(threeDiskInput("raid1c3", 2, 3)); p.kind != rebuildReturned {
			t.Fatalf("plan = %+v, want the rebuild: every chunk has a third copy on the disk that stayed", p)
		}
	})
	t.Run("the weakest profile decides", func(t *testing.T) {
		in := threeDiskInput("raid1c3", 2, 3)
		in.fs.Allocation[0].Profile = "raid1" // data
		if p := decideRebuild(in); p.kind != rebuildRefuse {
			t.Fatalf("plan = %+v, want a refusal for the raid1 data", p)
		}
	})
	t.Run("single chunks a degraded mount wrote do not count", func(t *testing.T) {
		in := threeDiskInput("raid1", 3)
		in.fs.Allocation = append(in.fs.Allocation, BtrfsAllocation{Kind: "data", Profile: "single"})
		if p := decideRebuild(in); p.kind != rebuildReturned {
			t.Fatalf("plan = %+v, want the rebuild: oneCopyChunks judges those by member", p)
		}
	})
}

// The hot spare is held to the same rule: a replace onto it could not copy
// what two missing members held together.
func TestRebuildOntoTheSpareWaitsForThePoolToBeWithinItsTolerance(t *testing.T) {
	spare := &BtrfsSpare{Disk: BtrfsSpareDisk{Serial: "SPARE", SizeBytes: 1000}, Device: "/dev/sdy", State: SpareReady}
	in := threeDiskInput("raid1", 2, 3)
	in.state.NoAutoRebuild, in.fs.Spare = true, spare
	if p := decideRebuild(in); p.kind != rebuildRefuse {
		t.Fatalf("plan with two missing = %+v, want the spare left unused", p)
	}
	in = threeDiskInput("raid1", 3)
	in.state.NoAutoRebuild, in.fs.Spare = true, spare
	if p := decideRebuild(in); p.kind != rebuildSpare {
		t.Fatalf("plan with one missing = %+v, want the spare", p)
	}
}

// A rebuild on record that starts again obeys the same rule: its disk, never
// erased, is given back to the pool (the record goes), and one already erased
// waits, since nothing of the member is left on it to keep.
func TestRebuildRestartWaitsForThePoolToBeWithinItsTolerance(t *testing.T) {
	record := func(in *rebuildInput) rebuildRecord {
		rec := rebuildRecord{ID: 3, UUID: devUUID(3), Disk: memberDisk(3), Device: "/dev/sdx", Started: in.now.Add(-time.Minute).Unix(), Boot: "boot", Restart: true}
		in.state.Rebuild, in.state.Returned, in.recordDisk = &rec, nil, "/dev/sdx"
		return rec
	}
	t.Run("a disk never erased", func(t *testing.T) {
		in := threeDiskInput("raid1", 2, 3)
		record(&in)
		in.recordSB = &in.devices[0].sb
		if p := decideRebuild(in); p.kind != rebuildRefuse || !p.clearRecord || p.fail {
			t.Fatalf("plan = %+v, want the record set aside with the refusal", p)
		}
	})
	t.Run("a disk already erased", func(t *testing.T) {
		in := threeDiskInput("raid1", 2, 3)
		record(&in)
		if p := decideRebuild(in); p.kind != rebuildWait || p.record == nil || p.clearRecord || p.fail || !strings.Contains(p.reason, "devices 2 and 3 are missing") {
			t.Fatalf("plan = %+v, want the record kept, waiting for the other disk", p)
		}
	})
	t.Run("one member missing starts again", func(t *testing.T) {
		in := threeDiskInput("raid1", 3)
		record(&in)
		if p := decideRebuild(in); p.kind != rebuildResume {
			t.Fatalf("plan = %+v, want the rebuild started again", p)
		}
	})
}

// The alert of a refusal goes once the next run no longer refuses, also when
// that run only waits, as for the second sighting of a disk that is back.
func TestRebuildAlertOfARefusalGoesWhenTheRunWaits(t *testing.T) {
	stubAlerts(t)
	fs := BtrfsFilesystem{UUID: dataUUID, Label: "tank"}
	key := "rebuild-" + dataUUID
	raiseRebuildAlert(t.Context(), fs, poolState{}, rebuildPlan{kind: rebuildRefuse, reason: "device 1 counts errors"})
	if got := alertKeys(t)[key]; got != "error" {
		t.Fatalf("alert = %q after a refusal, want an error", got)
	}
	raiseRebuildAlert(t.Context(), fs, poolState{}, rebuildPlan{kind: rebuildWait, reason: "/dev/sdx is back and is rebuilt onto once 2 health checks in a row have seen it"})
	if _, raised := alertKeys(t)[key]; raised {
		t.Fatal("the refusal's alert stayed while the rebuild waits for its second sighting")
	}
}
