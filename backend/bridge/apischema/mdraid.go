package apischema

// MDArray is a Linux software RAID array as the kernel's md sysfs reports it.
type MDArray struct {
	Name            string     `json:"name"`            // kernel name, e.g. md127
	Path            string     `json:"path"`            // /dev/md127
	Level           string     `json:"level"`           // md/level: raid1, raid5, ...
	State           string     `json:"state"`           // md/array_state: clean, active, read-auto, ...
	MetadataVersion string     `json:"metadataVersion"` // md/metadata_version
	SizeBytes       uint64     `json:"sizeBytes"`       // /sys/block/<name>/size * 512
	RaidDisks       int        `json:"raidDisks"`       // md/raid_disks
	Degraded        int        `json:"degraded"`        // md/degraded (0 when the file is absent, e.g. raid0)
	SyncAction      string     `json:"syncAction"`      // md/sync_action: idle, check, repair, resync, recover, reshape, frozen; "" when absent
	SyncProgress    float64    `json:"syncProgress"`    // percent 0-100 from md/sync_completed "done / total"; 0 for "none"
	SyncSpeedBytes  uint64     `json:"syncSpeedBytes"`  // md/sync_speed is KiB/s; converted to bytes/s; 0 for "none"
	MismatchCount   uint64     `json:"mismatchCount"`   // md/mismatch_cnt
	Members         []MDMember `json:"members"`         // never nil; sorted by slot (spares/faulty, slot -1, last), then name
}

// MDMember is one component device of an md array (md/dev-<name>/).
type MDMember struct {
	Name   string   `json:"name"`   // nvme0n1p2 (sysfs "!" means "/")
	Path   string   `json:"path"`   // /dev/nvme0n1p2
	Slot   int      `json:"slot"`   // md/dev-X/slot; -1 for "none"
	State  []string `json:"state"`  // md/dev-X/state split on ",": in_sync, faulty, spare, want_replacement, replacement, write_error, blocked, ...; never nil
	Errors uint64   `json:"errors"` // md/dev-X/errors
}

type MDSyncActionRequest struct {
	Name   string `json:"name"`
	Action string `json:"action"` // check | repair | idle
}

type MDMemberRequest struct {
	Name   string `json:"name"`
	Member string `json:"member"` // member Name as returned in MDArray.Members
}

type MDMemberAddRequest struct {
	Name   string `json:"name"`
	Device string `json:"device"` // /dev/... path of a free disk or partition
}

type MDMemberReplaceRequest struct {
	Name   string `json:"name"`
	Member string `json:"member"`
	Device string `json:"device"`
}
