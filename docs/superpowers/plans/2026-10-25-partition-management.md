# Partition Management and Native Block-Device Inventory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create, format, mount and delete GPT partitions and initialize disks from Storage → Disks, and build the block-device inventory from sysfs + udev instead of `lsblk`.

**Architecture:**
- **`backend/common/btrfs`** owns every raw disk write: GPT editing, the kernel partition ioctls, and `mkfs`. It already holds the GPT code, the sector, wipe and command helpers, and the root loop-device harness.
- **The bridge** (`backend/bridge/handlers/storage`) owns three things:
  - the inventory reader (sysfs, `/run/udev/data`, `/proc/self/mountinfo`, `/proc/swaps`, `/etc/fstab`);
  - the in-use rules;
  - five privileged endpoints.
- **The frontend** adds a Partitions tab to the expanded drive in `DiskOverview`.

**Tech Stack:** Go (`golang.org/x/sys/unix`, `encoding/json/v2`), React + TanStack Query, generated `linuxio` API client.

**Spec:** `docs/superpowers/specs/2026-10-25-partition-management-design.md`

## Global Constraints

- Never create a git commit (AGENTS.md). Leave all changes uncommitted.
- Use Make targets only, never `go test`, `npm`, `vitest`, `tsc` or `gofmt` directly. Focused runs:
  - Go: `make test-go-quiet GO_TEST_PKGS=./common/btrfs/ GO_TEST_FLAGS='-run X'`. Paths are relative to `backend/`.
  - Frontend: `make test-frontend-quiet VITEST_FILE=src/...test.tsx`.
  - On failure, read the log in `.cache/test-logs/` before re-running.
- Do not hand-edit `frontend/src/api/generated/*`. Run `make generate` after changing apischema or routes.
- Use GPT only. Starts and ends align to 1 MiB (`partitionAlign`). User partitions are at least 1 MiB. Pool partitions keep `minPartition` (256 MiB) and `PartitionSlack` (1 GiB).
- Write the backup GPT copy before the primary. Both copies must validate and agree; if not, refuse with "repair it first".
- Formats are `ext4`, `xfs` and `btrfs` only. Label limits: ext4 16 bytes, xfs 12 bytes, btrfs 255 bytes. No `/` or control characters (as `ValidateLabel`).
- `mkfs` flags:
  - UUID: `mkfs.ext4 -U <uuid>`, `mkfs.xfs -m uuid=<uuid>`, `mkfs.btrfs -U <uuid>`. The UUID comes from `newUUID()`.
  - Label: `-L <label>` only when the label is non-empty.
  - Force only when overwriting: ext4 `-F` (exactly once, never `-F -F`), xfs `-f`, btrfs `-f`.
  - Never pass a discard opt-out.
- fstab line: `UUID=<uuid> <path> <type> defaults,nofail <pass>`. Pass is 2 for ext4 and 0 for every other type.
- `BLKPG_DEL_PARTITION` retries on `EBUSY` 10 times, 100 ms apart, honouring ctx. Then it returns an error wrapping `unix.EBUSY`.
- A new partition gets its first and last 2 MiB zeroed with the existing `wipeDeviceEdges`. Delete does not wipe.
- Logging (AGENTS.md):
  - One Info line per completed mutation, using exactly these messages: "created partition", "formatted partition", "deleted partition", "mounted filesystem", "initialized disk". Attributes include `device`; the error key is `error`.
  - Log or return an error, never both.
  - Client errors wrap `bridgeipc.ErrInvalidArgs`, `ErrNotFound` or `ErrConflict`.
- Every new route is `apischema.Privileged()`.
- New `StorageBlockDevice` fields are `omitzero`, so they are optional in TypeScript and existing fixtures stay valid.
- Frontend:
  - Style only through `components/ui` and `--app-*` variables.
  - No new `useMemo`, `useCallback` or `memo`.
  - Server state stays in TanStack Query.

## Refinements to the spec (decided while planning)

- **`mkfs` lives in `backend/common/btrfs/format.go`**, not in the bridge's `partitions.go`. It reuses `runCommand`, `newUUID` and the root loop-device harness, and keeps every raw disk write in one package.
- **The existing `btrfs` capability (btrfs-progs) already covers `mkfs.btrfs`.** Only `e2fsprogs` and `xfsprogs` are new.
- **The inventory gains `busy` and `inFstab`.** The UI then shows the same reasons the backend enforces, without re-implementing fstab or swap matching in TypeScript. btrfs pool membership still comes from the btrfs listings the page already loads.
- **btrfs mounts are matched by source path.** btrfs reports an anonymous `0:N` device in mountinfo, so matching mount points by `maj:min` alone misses them; a btrfs mount is matched by its source path instead.

## Review Focus

1. **Two table edits on one disk at once** (two browser tabs, or a create task plus a delete). Both must land and neither may be lost. GPT writes take one package mutex. Test: `TestConcurrentCreatePartitionsKeepBoth` (Task 1).
2. **Mount points and labels with spaces or non-ASCII.** mountinfo escapes them as `\040` and udev as `\x20`; both must still match and display. Tests: `TestReadMountinfoUnescapes` and `TestUdevEncodedLabel` (Task 2).
3. **Formatting again before udev refreshes**, while the UI still says "No filesystem". mkfs must refuse instead of overwriting, so no force flag is passed unless `overwrite`. Test: `TestFormatArgs` asserts no `-F`/`-f` when overwrite is false (Task 1).
4. **4K-logical-sector disks.** Byte offsets must convert to LBAs correctly. Test: the `4k sectors` case in `TestCreatePartitionAtRange` (Task 1).
5. **Disks whose name ends in a digit** (`nvme0n1`, `mmcblk0`). Partition numbers come from sysfs `partition`, never from parsing the name. Tests: the `nvme0n1p2` fixture in `TestReadBlockDevicesTree` (Task 2) and the nvme-named sysfs fixture in `TestInitializeDisk` (Task 1).

---

### Task 1: GPT create / delete / initialize, busy check and mkfs in `backend/common/btrfs`

**Files:**
- Modify: `backend/common/btrfs/partition.go`
- Create: `backend/common/btrfs/format.go`
- Test: `backend/common/btrfs/partition_test.go`, `backend/common/btrfs/format_test.go`

**Interfaces:**
- Consumes: existing `diskGeometry`, `readGPTCopy`, `gptCopy.withEntries`, `addKernelPartition`, `waitForNode`, `partitionPath`, `wipeDeviceEdges`, `rereadPartitions`, `sysBlockDir` (pool.go), `runCommand`, `newUUID`, `execCommand`.
- Produces:
  - `func FreeRegions(firstUsable, lastUsable uint64, used [][2]uint64, sectorSize, minBytes uint64) [][2]uint64`: inclusive LBA ranges, aligned, each at least `minBytes`, in disk order. `LargestFreeRegion` keeps its signature and becomes the longest result of `FreeRegions(..., minPartition)`.
  - `const MinUserPartition = partitionAlign`
  - `func CreatePartition(ctx context.Context, device string, startBytes, sizeBytes uint64) (string, error)`: returns the partition path.
  - `func DeletePartition(ctx context.Context, device string, number int) error`
  - `func InitializeDisk(ctx context.Context, device string) error`
  - `func CheckNotBusy(device string) error`: `open(O_RDONLY|O_EXCL)`; on `EBUSY` it returns `fmt.Errorf("%s is in use: %w", device, unix.EBUSY)`.
  - `func ValidateFormat(fsType, label string) error`
  - `func Format(ctx context.Context, device, fsType, label string, overwrite bool) (uuid string, err error)`
  - Test seam: `var delKernelPartition = func(device string, number int) error`

- [ ] **Step 1: Write the failing tests** in `partition_test.go`. Reuse `writeTestDisk` and the existing stubs for `sectorSize`, `addKernelPartition`, `rereadPartitions` and `wipeDeviceEdges`, the way `TestAddPartitionUsesLargestGap` does.
  - `TestFreeRegions`: table cases.
    - Empty disk gives one region.
    - Two gaps come back in disk order.
    - A gap under `minBytes` is dropped.
    - Unaligned neighbours are trimmed to 1 MiB.
    - 4096-byte sectors.
    - `TestLargestFreeRegion` still passes unchanged.
  - `TestCreatePartitionAtRange`: cases `512 sectors` and `4k sectors`.
    - Image of 4 GiB with one partition at LBA 2048–(1 GiB).
    - Request `startBytes = 2 GiB + 1` and `sizeBytes = 512 MiB + 7`.
    - Assert that both copies (`readGPTCopy` at 1 and at last) list the new entry in slot 2. It starts at the next 1 MiB boundary and ends at the last 1 MiB boundary inside the request.
    - Assert that `addKernelPartition` got `(device, 2, firstLBA*ss, lenLBA*ss)`.
    - Assert that `wipeDeviceEdges` got `partitionPath(device, 2)`.
    - Assert that the return value is that path.
  - `TestCreatePartitionRefuses`: each case leaves the image byte-identical.
    - Overlaps the existing partition.
    - Ends past `lastUsable`.
    - Under 1 MiB after rounding.
    - Copies differ.
  - `TestConcurrentCreatePartitionsKeepBoth`: two goroutines create in two different free regions. Afterwards both entries are present in both copies.
  - `TestDeletePartition`:
    - The `delKernelPartition` stub asserts the image is still unchanged when it is called.
    - Afterwards entry 1 is all zero in both copies, both copies validate, and every byte outside the two entry slots and the two header sectors is unchanged.
  - `TestDeletePartitionBusy`:
    - With the stub always returning `unix.EBUSY`: `errors.Is(err, unix.EBUSY)`, 10 calls, image unchanged.
    - With the stub returning `EBUSY` twice then nil, the delete succeeds.
  - `TestInitializeDisk`:
    - Image with two partitions. Fake `sysBlockDir` tree `nvme0n1/nvme0n1p1/{partition,start,size}` and `nvme0n1/nvme0n1p2/...`; the device path's base name is `nvme0n1`, as a symlink in the temp dir.
    - Afterwards both copies validate with zero entries, and `rereadPartitions` was called once.
    - Busy case: the stub fails on p2. p1 is re-added through `addKernelPartition` with its sysfs `start*512` and `size*512`, the image is unchanged, and `errors.Is(err, unix.EBUSY)`.

- [ ] **Step 2: Write the failing tests** in `format_test.go`, using `stubExec` from `pool_test.go`.
  - `TestFormatArgs`: table over `{fsType, label, overwrite}`. Assert the exact argv:
    - `mkfs.ext4 -U <u> -L data /dev/x`
    - `mkfs.ext4 -U <u> -F /dev/x` (overwrite, no label)
    - `mkfs.xfs -m uuid=<u> -f /dev/x`
    - `mkfs.btrfs -U <u> -L data -f /dev/x`

    No `-F`/`-f` appears without overwrite. `<u>` equals the returned UUID.
  - `TestValidateFormat`: rejects `vfat`; ext4 with a 17-byte label; xfs with a 13-byte label; btrfs with 256 bytes; a label containing `/` or `\n`. Accepts `""` and the limits exactly.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `make test-go-quiet GO_TEST_PKGS=./common/btrfs/ GO_TEST_FLAGS='-run "FreeRegions|CreatePartition|DeletePartition|InitializeDisk|Format"'`
Expected: FAIL. The build fails because `FreeRegions`, `CreatePartition` and the rest are undefined.

- [ ] **Step 4: Implement** in `partition.go`:
  - **Refactor.** Split `AddPartition` into `readTables(f, device) (ss, last uint64, primary, backup gptCopy, err)` and `writeEntry(f, ss, last, primary, backup, slot, entry []byte)`. The latter does the backup-then-primary writes and `Sync`. `AddPartition` = `LargestFreeRegion` + the shared create path, so it now also wipes the new partition's edges.
  - **Locking.** A package `var tableMu sync.Mutex` is held by `AddPartition`, `CreatePartition`, `DeletePartition`, `InitializeDisk` and `PartitionDisk`.
  - **`CreatePartition`.** Round the request inward to `partitionAlign`. Require that the result is at least `MinUserPartition` and lies inside one `FreeRegions(primary.firstUsable, primary.lastUsable, used, ss, partitionAlign)` region. Use the first empty slot. After `waitForNode`, call `wipeDeviceEdges(part)`.
  - **`delKernelPartition`.** Same shape as `addKernelPartition`, with `Op: unix.BLKPG_DEL_PARTITION` and only `Pno` set.
  - **`DeletePartition`.**
    1. Entry `number-1` must be non-empty.
    2. Call `delKernelPartition` with the retry from the Global Constraints.
    3. Write a zero entry with `writeEntry`.
    4. If the write fails, call `addKernelPartition(device, number, first*ss, len*ss)` and return the write error, wrapped with the re-add error if the re-add also fails.
  - **`InitializeDisk`.**
    1. List kernel partitions as the subdirectories of `sysBlockDir/<base(device)>` that contain a `partition` file.
    2. Drop each with the retry. On failure, re-add the ones already dropped and return.
    3. Call `wipeDeviceEdges(device)`.
    4. Write `gptLayout{sectorSize, sectors, disk: randomGUID()}.build()`; make `entries()` return all zeros when `last == 0`.
    5. Refuse disks under 2 MiB.
    6. Call `rereadPartitions(device)`.
  - **`CheckNotBusy`.**

  In `format.go`, implement `ValidateFormat` and `Format`: validate, then `uuid := newUUID()`, then `runCommand(ctx, "mkfs."+fsType, args...)`.

- [ ] **Step 5: Run the package tests to verify they pass**

Run: `make test-go-quiet GO_TEST_PKGS=./common/btrfs/`
Expected: PASS, including every pre-existing partition and pool test.

### Task 2: Native inventory reader replacing `lsblk`

**Files:**
- Modify: `backend/bridge/handlers/storage/block_devices.go` (the reader replaces `runLsblk`, `parseBlockDevices` and `collectBlockDevice`).
- Modify: `backend/bridge/handlers/storage/drives.go`: derive drives from the reader; delete `LSBLKOutput`, `BlockDevice` and `buildDriveInfo`.
- Modify: `backend/bridge/apischema/storage.go` (`ApiDisk`, `StorageBlockDevice`, new `StorageRegion`).
- Modify: `backend/bridge/handlers/storage/types.go` (the `DriveInfo` comment).
- Test:
  - `backend/bridge/handlers/storage/block_devices_test.go`: rewrite it. The old lsblk JSON parser moves here as a test helper, used only by the parity test.
  - `backend/bridge/handlers/storage/drives_test.go`
- Frontend (contract consumers):
  - `frontend/src/routes/_authenticated/storage/-components/disk-overview/{DiskOverview.tsx,DriveInfoTab.tsx,utils.ts,utils.test.ts}`
  - `frontend/src/routes/_authenticated/-dashboard/Drive.tsx`
  - `frontend/src/test/browser/fixture/routes/StorageDisksPage.tsx`
  - every test fixture that builds an `ApiDisk`
- Docs: `docs/storage-disks.md` § Inventory.

**Interfaces:**
- Consumes: `btrfs.FreeRegions`, `btrfs.LargestFreeRegion`, `btrfs.MinUserPartition`; `getFstabEntries` (fstab.go).
- Produces:
  - `apischema.ApiDisk`: `Size string` is replaced by `SizeBytes uint64 \`json:"sizeBytes"\``.
  - `apischema.StorageRegion{StartBytes, SizeBytes uint64}` (JSON `startBytes`, `sizeBytes`).
  - `apischema.StorageBlockDevice` gains, all `omitzero`:
    - `PTType string \`json:"ptType"\``
    - `PartNumber int \`json:"partNumber"\``
    - `StartBytes uint64 \`json:"startBytes"\``
    - `UUID string \`json:"uuid"\``
    - `Label string \`json:"label"\``
    - `PartUUID string \`json:"partUUID"\``
    - `PartLabel string \`json:"partLabel"\``
    - `Busy string \`json:"busy"\``: one of "Mounted at <path>", "Active swap", "Used by <holder path>", in that priority.
    - `InFstab bool \`json:"inFstab"\``
    - `FreeRegions []StorageRegion \`json:"freeRegions"\``: GPT disks only, regions of at least `MinUserPartition`.
  - `FreeBytes` keeps its current meaning.
  - Reader seams (package vars): `sysClassBlockRoot` (exists), `udevDataRoot = "/run/udev/data"`, `procMountinfo = "/proc/self/mountinfo"`, `procSwaps = "/proc/swaps"`, `devRoot = "/dev"`.
  - `func ListBlockDevices(ctx context.Context) ([]apischema.StorageBlockDevice, error)` and `func FetchDriveInfo(ctx context.Context) ([]DriveInfo, error)` keep their signatures.

- [ ] **Step 1: Write the failing tests** in `block_devices_test.go`. Build a temp tree with `sys/class/block`, `run/udev/data`, `mountinfo`, `swaps` and `dev` symlinks, using a helper `fakeHost(t)`.
  - `TestReadBlockDevicesTree`. Devices: `sda`, `sda1` (part 1, ext4, mounted `/data`), `sda2` (swap in `/proc/swaps`), `sdb` (whole-disk md member), `md0` (`md/level` raid1, `slaves/sda3`, `slaves/sdb`), `dm-0` (`dm/name` `vg-root`, `dm/uuid` `LVM-x`, `slaves/md0`), `nvme0n1` and `nvme0n1p2` (btrfs, mounted `/` via a mountinfo line whose maj:min is `0:31` and source `/dev/nvme0n1p2`), `loop0`, `ram0` (major 1), `sr0` with size 0 (kept, as lsblk keeps empty devices), and `nvme0c0n1` with `hidden` = 1. Assert:
    - `sda1.Mountpoints == ["/data"]` and `Busy == "Mounted at /data"`.
    - `sda2.Busy == "Active swap"`.
    - `sdb.Busy == "Used by /dev/md0"`.
    - `md0.Type == "raid1"` with parents `["/dev/sda3", "/dev/sdb"]`.
    - `dm-0` has `Name "vg-root"`, `Path "/dev/mapper/vg-root"`, `Type "lvm"`, `KernelName "dm-0"`.
    - `nvme0n1p2` has `PartNumber 2`, `Parents ["/dev/nvme0n1"]`, `Mountpoints ["/"]`.
    - loop0, ram0, the empty sr0 and the hidden device are absent.
    - Every slice is non-nil.
  - `TestReadMountinfoUnescapes`: the mount point `/mnt/my\040disk` shows up as `/mnt/my disk`.
  - `TestUdevEncodedLabel`: `E:ID_FS_LABEL_ENC=my\x20data` gives `Label "my data"`. Model comes from `ID_MODEL_ENC`, then `ID_MODEL`, then sysfs `device/model`. Serial follows `SCSI_IDENT_SERIAL` > `ID_SCSI_SERIAL` > `ID_SERIAL_SHORT` > `ID_SERIAL` > `device/serial`. `PTType` falls back to `ID_PART_ENTRY_SCHEME`. A missing udev file leaves the fields empty without an error.
  - `TestTransport`: table over fake sysfs, one case per branch. spi, fc, fcoe, sas (host and `sas_device`), sbp, iscsi, usb (the SCSI device link contains `/usb`), sata (`proc_name` `ahci`), ata, nvme, virtio (`vd*`), mmc, and unknown → "".
  - `TestInFstab`: each fstab source form marks `/dev/sda1` as `InFstab`:
    - `UUID=`
    - `PARTUUID=` (case-insensitive)
    - `LABEL=`
    - `PARTLABEL=`
    - `/dev/sda1`
    - a `devRoot/disk/by-id/x` symlink to `../../sda1`

    A different UUID does not mark it.
  - `TestAddFreeSpace` (keep it, adapted). It also asserts that `FreeRegions` lists both gaps in disk order and that `FreeBytes` is unchanged.
  - `TestNativeInventoryMatchesLsblk`: the parity test.
    - Skip unless `lsblk` is on PATH and `/run/udev/data` exists.
    - Run `lsblk --json --bytes --tree --output NAME,KNAME,PATH,TYPE,SIZE,FSTYPE,MOUNTPOINTS,MODEL,SERIAL,TRAN,RO,PTTYPE` and parse it with the moved helper.
    - Require `ListBlockDevices` to be equal on every device for Path, Name, KernelName, Type, SizeBytes, FSType, Mountpoints, Parents, Model, Serial, Transport, ReadOnly and PTType.
    - Repeat for `FetchDriveInfo` against `lsblk -d -b -J -o NAME,MODEL,SERIAL,SIZE,RO,ROTA,TYPE,TRAN,VENDOR`.
  - In `drives_test.go`, `TestFetchDriveInfoFromTree` covers only disks, with `SizeBytes`, `Rotational`, `Vendor` and `Type` (the transport).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/storage/ GO_TEST_FLAGS='-run "ReadBlockDevices|Mountinfo|Udev|Transport|InFstab|AddFreeSpace|Lsblk|FetchDriveInfo"'`
Expected: FAIL, because the new fields and seams are undefined.

- [ ] **Step 3: Implement the reader** in `block_devices.go`. It follows the field table in the spec's "Native inventory" section and these rules:
  - Skip `loop*`, major 1, and devices whose `hidden` is 1. Keep empty (size 0) devices: lsblk lists them by default and hides them only with `--noempty` (ruling after the Task 2 review).
  - Device paths are always `/dev/<kname>` or `/dev/mapper/<name>`, never built from `devRoot`. `devRoot` is only where sources are resolved:
    1. A `/dev/...` source from mountinfo, swaps or fstab is rewritten under `devRoot`.
    2. It is resolved with `filepath.EvalSymlinks`.
    3. The result is mapped back to `/dev/<rel>`.
    4. When resolution fails, the raw source is compared instead.
  - Mount points: match mountinfo by `maj:min`. Otherwise (major 0, as for btrfs) match by the resolved source.
  - Swap: match `/proc/swaps` filenames the same way.
  - `InFstab` comes from `getFstabEntries()`.
  - Free regions are computed as in `gptFreeBytes`, but with `FreeRegions(..., btrfs.MinUserPartition)`.
  - `FetchDriveInfo` filters `Type == "disk"`. Vendor and rotational are read alongside.
  - Remove the lsblk subprocess and the `hostcmd` import if it becomes unused.

- [ ] **Step 4: Run the tests to verify they pass.** Use the same command as Step 2, then `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/storage/`.
Expected: PASS. Confirm the parity test **ran, not skipped**, on this host (the log shows `--- PASS: TestNativeInventoryMatchesLsblk`).

- [ ] **Step 5: Regenerate the contract and update the frontend consumers.**
  - Run `make generate`.
  - Replace `parseSizeToBytes(d.size)` and `parseSizeToBytes(raw.size)` with `.sizeBytes` in `DiskOverview.tsx` and `-dashboard/Drive.tsx`.
  - In `DriveInfoTab`, drop `rawDriveSize`; Capacity becomes `formatFileSize(drive.sizeBytes)`.
  - Delete `parseSizeToBytes` and its tests from `utils.ts` and `utils.test.ts`.
  - Update every `ApiDisk` fixture from `size: "<x>"` to `sizeBytes: <n>`. Find them with `rg -n 'size: "' frontend/src`.

- [ ] **Step 6: Rewrite `docs/storage-disks.md` § Inventory.** Name the sources (sysfs, `/run/udev/data`, mountinfo, swaps). State that systemd does not promise the udev database format. Note that devices udev has not probed yet show no filesystem until the next refresh. Mention that the parity test exists.

- [ ] **Step 7: Verify across the boundary**

Run: `make check-backend-quiet` then `make check-frontend-quiet`. Not `make test-quiet`: its `check-docs` step fails until Tasks 3 and 4 create the files the spec and plan name.
Expected: PASS.

### Task 3: Bridge endpoints, in-use rules, unmount fstab option, capabilities

**Files:**
- Create: `backend/bridge/handlers/storage/partitions.go` and `backend/bridge/handlers/storage/partitions_test.go`
- Modify:
  - `backend/bridge/apischema/storage.go`: request types.
  - `backend/bridge/handlers/storage/handlers.go`: register the routes, and switch `storage.unmount_filesystem` to `MountpointRemoveFstabRequest`.
  - `backend/bridge/handlers/storage/filesystems.go`: `UnmountFilesystem(ctx, mountpoint string, removeFstab bool)` calls `removeFromFstab` after a successful unmount.
  - `backend/bridge/handlers/storage/fstab.go`: `addToFstab` writes pass `2` when fstype is `ext4` and `0` otherwise.
- Modify (capabilities `e2fsprogs` → `mkfs.ext4` and `xfsprogs` → `mkfs.xfs`; the existing `btrfs` capability covers `mkfs.btrfs`): follow the `smartmontools` entry in each of these files:
  - `backend/bridge/handlers/capabilities/registry.go` (`checkDependencyCommand`; Install packages `e2fsprogs` and `xfsprogs` on both Debian and RHEL)
  - `backend/bridge/handlers/capabilities/response.go`
  - `backend/common/session/session.go`
  - `backend/webserver/auth/auth_handlers_test.go`
  - `frontend/src/api/capabilities.ts` (states `e2fsprogsAvailable` and `xfsprogsAvailable`; route to `/storage`)
  - `docs/capabilities.md`

**Interfaces:**
- Consumes: everything Task 1 produces; `ListBlockDevices` with `Busy`, `InFstab`, `FreeRegions` and `PartNumber` (Task 2); `btrfs.ListFilesystems`; `ListOfflinePools` (btrfs_pool.go); `checkPoolMountpoint`; `addToFstab`; `BtrfsOpProgress`.
- Produces (apischema):
  - `PartitionFormatOptions{FSType, Label string}`, JSON `fsType`, `label`.
  - `PartitionMountOptions{Mountpoint string; Persist bool}`, JSON `mountpoint`, `persist`.
  - `PartitionCreateRequest{Device string; StartBytes, SizeBytes uint64; Format *PartitionFormatOptions \`omitzero\`; Mount *PartitionMountOptions \`omitzero\`}`
  - `PartitionFormatRequest{Device, FSType, Label string; Overwrite bool}`
  - `PartitionMountRequest{Device, Mountpoint string; Persist bool}`
  - `BlockDeviceRequest{Device string}`
- Produces (routes):

  | Route | Kind | Types | Notes |
  |---|---|---|---|
  | `storage.partition_create` | `TaskRunner` | `PartitionCreateRequest` → `SuccessPathResponse` | options `Privileged(), SessionTask()`; progress `BtrfsOpProgress` with `Op: "partition"`; metadata `{Identity: [Device, "partition"], Label: "Creating partition", Device}`; policy `bridgeipc.TaskDefault` (since 1b1f539a it refuses a second task with the same identity) |
  | `storage.partition_format` | `TaskRunner` | `PartitionFormatRequest` → `SuccessResponse` | identity `[Device, "format"]`, label "Formatting partition", `Device` |
  | `storage.partition_delete` | `Call` | `BlockDeviceRequest` → `SuccessResponse` | |
  | `storage.partition_mount` | `Call` | `PartitionMountRequest` → `StorageMountResult` | |
  | `storage.disk_initialize` | `Call` | `BlockDeviceRequest` → `SuccessResponse` | |

- Test seams (package vars), following the `mdBlockDevices` pattern:

  ```go
  storageBlockDevices = ListBlockDevices
  btrfsFilesystems = btrfs.ListFilesystems
  offlinePools = ListOfflinePools
  createPartition = btrfs.CreatePartition
  deletePartition = btrfs.DeletePartition
  initializeDisk = btrfs.InitializeDisk
  formatDevice = btrfs.Format
  checkNotBusy = btrfs.CheckNotBusy
  mountDevice = unix.Mount
  ```

- [ ] **Step 1: Write the failing tests** in `partitions_test.go`. Use a fixture inventory: a GPT disk `/dev/sdb` with a free region at 1–2 GiB and partitions `sdb1` (free, ext4, UUID `u1`) and `sdb2`. Stub every seam to record calls.
  - `TestDeviceInUse`: one table case per reason, each returning an error that `errors.Is(err, bridgeipc.ErrConflict)` and whose message contains the reason:
    - `Busy` "Mounted at /x"
    - "Active swap"
    - "Used by /dev/md0"
    - `InFstab`
    - member of a mounted btrfs filesystem
    - member of an offline pool

    A free partition returns nil. For `forMount && !persist`, `InFstab` is ignored.
  - `TestCreatePartitionValidatesBeforeWriting`: each case gives `ErrInvalidArgs` or `ErrConflict`, and the `createPartition` stub is never called.
    - Unknown device.
    - Device not a disk.
    - Read-only disk.
    - `PTType "dos"` ("initialize it first").
    - Range outside every free region.
    - Size under 1 MiB.
    - Bad label for the fs type.
    - Mount point `/etc`.
  - `TestCreatePartitionFormatsAndMounts`:
    - Steps reported in order: "Partitioning", "Formatting: SSDs are trimmed first, which can take a few minutes", "Mounting at /srv/new".
    - `formatDevice` is called with `overwrite=false`.
    - `mountDevice` is called with `(part, "/srv/new", "ext4", 0, "")`.
    - The fstab gets `UUID=<returned uuid> /srv/new ext4 defaults,nofail 2`, through a temp `fstabPath`.
  - `TestFormatPartition`:
    - An existing signature without overwrite gives `ErrConflict` and no format.
    - A `formatDevice` stub returning `btrfs.ErrDeviceHasData` (the partition holds data that udev did not report) gives `ErrConflict`.
    - `checkNotBusy` returning `unix.EBUSY` gives `ErrConflict`.
    - With overwrite, `formatDevice` gets `overwrite=true`.
  - `TestDeletePartition`:
    - Calls `deletePartition("/dev/sdb", 1)`, taking the number from `PartNumber`.
    - A stub `unix.EBUSY` maps to `ErrConflict`.
    - A partition on a non-GPT disk gives `ErrInvalidArgs`.
  - `TestMountPartition`:
    - `persist` with `InFstab` gives `ErrConflict`.
    - Without `persist`, the mount succeeds and the fstab is untouched.
    - An offline-pool member gives `ErrConflict`.
    - `persist` with an empty UUID gives `ErrInvalidArgs`.
    - A non-empty existing mount point gives `ErrConflict`.
  - `TestInitializeDisk`: a disk with a mounted partition gives `ErrConflict` and no call. A free disk calls `initializeDisk`.
  - `TestUnmountRemovesFstab` (in `filesystems` or `fstab_test.go`): with `removeFstab` the line is gone; without it, the line stays.
  - `TestAddToFstabPass` (in `fstab_test.go`): ext4 gives `... 0 2`, btrfs gives `... 0 0`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/storage/ GO_TEST_FLAGS='-run "DeviceInUse|CreatePartition|FormatPartition|DeletePartition|MountPartition|InitializeDisk|Unmount|AddToFstabPass"'`
Expected: FAIL, because these functions are undefined.

- [ ] **Step 3: Implement `partitions.go`.**
  - **In-use check.** `func deviceInUse(dev apischema.StorageBlockDevice, fss []btrfs.BtrfsFilesystem, offline []btrfs.BtrfsOfflinePool, forMount, persist bool) error`. It returns `fmt.Errorf("%w: %s: %s", bridgeipc.ErrConflict, dev.Path, reason)`.
  - **Read-only disks.** Create, Format, Delete and Initialize refuse a read-only disk (for a partition, its parent disk) with `ErrConflict`. Mount is exempt and lets `mount(2)` report the error.
  - **udev lock (ruling after Task 1).**
    - Every mutation except Mount holds `unlock, err := btrfs.LockDisk(ctx, diskPath)` on the whole disk (a partition's parent) for its whole duration: create through format and mount, Format, Delete and Initialize. Release it with `defer unlock()`.
    - Why: closing a whole-disk fd that was opened for writing makes systemd-udevd re-read the partition table (BLKRRPART) when no partition is open, and every partition node then disappears and reappears just as mkfs starts. The lock makes udevd skip that and postpone probing (systemd.io/BLOCK_DEVICE_LOCKING).
    - Take it after validation and before the first write. Never take it twice for one disk in the same request.
    - `CreateBtrfsPool` (btrfs_pool.go) holds it on each disk it partitions (Partition or FreeSpace) from before `PartitionDisk` or `AddPartition` until `CreatePool` returns, because it has the same race.
    - Add the seam `lockDisk = btrfs.LockDisk`. Tests assert it is taken once per mutation, on the parent disk, before any write.
  - **Order of operations in every handler:**
    1. Load the inventory and btrfs lists, and validate everything.
    2. Make the kernel busy check where the spec requires it.
    3. Then write.
  - **`CreatePartition`** validates format and mount options before calling `createPartition`. A failure after the partition exists is reported as `partition %s was created but not formatted` or `... formatted but not mounted`.
  - **`MountPartition`:**
    - It requires a non-empty `FSType` and passes `nil` fstab to `checkPoolMountpoint` when `!persist`.
    - `checkNotBusy(dev.Path)` must pass.
    - If the mount point exists, it must be empty. Otherwise it is created with `os.MkdirAll(…, 0o755)`.
    - The mount is `mountDevice(dev.Path, mp, dev.FSType, 0, "")`.
    - With `persist`, the fstab entry is added via `addToFstab("UUID="+dev.UUID, mp, dev.FSType, []string{"defaults","nofail"})`.
  - **Errors from the btrfs layer** are wrapped with `%w`. `unix.EBUSY` and `btrfs.ErrDeviceHasData` become `ErrConflict`. Since Task 1, `btrfs.Format` refuses data found in the first 1 MiB when `overwrite` is false.

  Then register the routes. Task runners go in a new `partitionBindings()` added to `Routes` via `CombineRoutes`; calls go in `api`. Add the capabilities.

- [ ] **Step 4: Run the package tests to verify they pass**

Run: `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/storage/` then `make test-go-quiet GO_TEST_PKGS=./bridge/handlers/capabilities/ ./webserver/auth/`
Expected: PASS.

- [ ] **Step 5: Regenerate, then lint the backend**

Run: `make generate`, then `make check-backend-quiet`, then `make check-frontend-quiet`.
Expected: PASS. Keep the frontend compiling in this task:
- The generated `storage.unmount_filesystem` request now carries `removeFstab`. Pass `removeFstab: ""` at the existing call in `DiskOverview.tsx`; Task 4 adds the checkbox.
- Fix any `capabilities.ts` drift tests here.
- If `docs/api-contract.md` states how many Task routes declare `SessionTask()`, recount with `rg` and update the number.

### Task 4: Partitions tab, dialogs, unmount option, invalidation

**Files:**
- Create: `frontend/src/routes/_authenticated/storage/-components/disk-overview/partitionLayout.ts` + `partitionLayout.test.ts`
- Create: `frontend/src/routes/_authenticated/storage/-components/disk-overview/PartitionsTab.tsx` + `PartitionsTab.test.tsx`
- Create: `frontend/src/routes/_authenticated/storage/-components/disk-overview/PartitionDialogs.tsx`. It exports `CreatePartitionDialog`, `FormatPartitionDialog` and `MountPartitionDialog`, which share `FormatFields` and `MountFields`.
- Modify: `DiskOverview.tsx`:
  - add a "Partitions" tab right after Overview, renumbering the TabPanel indices;
  - turn the Unmount button into a confirmation dialog with an "Also remove from /etc/fstab" `AppCheckbox`, sending `removeFstab: "true"` when ticked. Build it from `AppDialog`, `AppDialogTitle`, `AppDialogContent` and `AppDialogActions`, like the pool destroy dialog in `StoragePools.tsx`. `AppConfirmDialog` takes only a message string, so it cannot hold the checkbox.
- Modify: `frontend/src/api/operation-query-invalidations.ts` (+ its test): `PARTITION_KEYS = BTRFS_POOL_KEYS` for the five new routes.
- Modify: `frontend/src/tasks/taskTypes.ts`: `TASK_TYPE_STORAGE_PARTITION_CREATE` and `TASK_TYPE_STORAGE_PARTITION_FORMAT`.

**Interfaces:**
- Consumes: the generated `linuxio.storage.partition_create`, `partition_format`, `partition_delete`, `partition_mount`, `disk_initialize` and `get_topology`, `btrfs_list`, `btrfs_pools_offline` (gated exactly as in `StoragePools.tsx`: `privileged && btrfsAvailable`); `useCapability("e2fsprogsAvailable" | "xfsprogsAvailable" | "btrfsAvailable")`; `AppConfirmDialog`; `useTaskStreamAction`; `useActiveTaskRecovery` (matched on `task.metadata?.device === disk.path`, or a partition of it, for format).
- Produces: `diskLayout(disk: StorageBlockDevice, devices: StorageBlockDevice[], btrfs: BtrfsFilesystem[], offline: BtrfsOfflinePool[]): LayoutRow[]`, where `LayoutRow = { kind: "partition"; device: StorageBlockDevice; reason: string | null } | { kind: "free"; startBytes: number; sizeBytes: number }`, sorted by start. The reason priority is `busy`, then "In /etc/fstab", then "Member of btrfs pool <label or uuid>". Also produces `initializeBlocker(disk, rows, btrfs, offline): string | null`.

- [ ] **Step 1: Write the failing tests**
  - `partitionLayout.test.ts`:
    - Rows interleave partitions and free regions by start.
    - Reasons follow the priority.
    - An offline-pool member is flagged.
    - `initializeBlocker` returns the first busy partition's reason, "Read-only disk" for `readOnly`, and null for a free disk.
  - `PartitionsTab.test.tsx`, mocking the API the way `StoragePools.test.tsx` does:
    - A busy row's Delete and Format are disabled, and their tooltip shows the reason.
    - Create on a free row opens the dialog with the size defaulted to the region.
    - Typing a size over the region clamps it to the maximum.
    - Submitting sends `{device, startBytes, sizeBytes}`, with the size rounded down to a whole MiB.
    - Choosing ext4 adds `format: {fsType: "ext4", label}`.
    - The mount section appears only with a format.
    - An ext4 label over 16 bytes shows an inline error and disables submit.
    - The Format dialog always shows an "Overwrite existing data" checkbox. Submit stays disabled until it is ticked when the partition has an `fsType`. Without an `fsType` it is optional: the backend still refuses a partition holding data (`ErrConflict`), and the error tells the user to tick it.
    - An unavailable xfs capability shows xfs as unavailable.
    - Delete and Initialize confirmations name the partition, its size and its `fsType`.
    - An MBR disk (`ptType: "dos"`) shows partitions with no Create or Delete, plus Initialize.
  - In `operation-query-invalidations.test.ts`, assert that the five routes map to `BTRFS_POOL_KEYS`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-frontend-quiet VITEST_FILE=src/routes/_authenticated/storage/-components/disk-overview/partitionLayout.test.ts` and the same with `PartitionsTab.test.tsx`.
Expected: FAIL, because the modules are not found.

- [ ] **Step 3: Implement.**
  - Create and Format use `useTaskStreamAction`, showing the progress `detail.message` as the step text (the `StoragePools` pool-create pattern).
  - Delete, Mount and Initialize use `useCallMutation` with success toasts: "Partition deleted", "Filesystem mounted" and "Disk initialized".
  - The size input offers MiB, GiB and TiB.
  - The UI label limits come from one constant map `{ext4: 16, xfs: 12, btrfs: 255}`, measured in UTF-8 bytes.
  - Show the disabled reasons with `AppTooltip`.

- [ ] **Step 4: Run the tests to verify they pass.** Use the same two commands, then `make test-frontend-quiet VITEST_FILE=src/api/operation-query-invalidations.test.ts`.
Expected: PASS.

- [ ] **Step 5: Run the frontend gates**

Run: `make check-frontend-quiet`
Expected: PASS (lint, tsc, the styling-boundary guard, unit tests).

- [ ] **Step 6: Run the browser suite.** Run `make test-frontend-browser-quiet`. If Disks-page specs fail on the new tab, adjust `frontend/src/test/browser/fixture/routes/StorageDisksPage.tsx`.
Expected: PASS.

### Task 5: Root loop-device test and docs

**Files:**
- Modify: `backend/common/btrfs/integration_test.go`: new `TestPartitionIntegration` on its own `loopDevice` of 2 GiB, using the existing helpers.
- Modify: `mk/quality.mk`: the `test-btrfs-integration` run regex becomes `'^Test(Btrfs(Degraded)?|Partition)Integration$$'`, and its help text adds "and partition".
- Modify: `docs/storage-disks.md` (new § Partition management, summarizing the spec's UX table and safety rules), `docs/btrfs.md` (pool partitioning shares this code; delete is now possible from Disks), `docs/development.md` (only if it lists what `make test-btrfs-integration` covers).

**Interfaces:**
- Consumes: `InitializeDisk`, `CreatePartition`, `Format`, `CheckNotBusy`, `DeletePartition`, `LockDisk` and `readGPTCopy` from Task 1, and `loopDevice` from the existing harness.
- Hold `LockDisk` on the loop disk around each mutation the way the bridge does. Run step 2's second `CreatePartition` and step 3's `Format` back to back under one lock, because that is the udev race the lock exists for.

- [ ] **Step 1: Write `TestPartitionIntegration`**, gated like `TestBtrfsIntegration` (env var plus root). Steps:
  1. `InitializeDisk`. Assert that both copies have zero entries and no `loopNp*` sysfs entries exist.
  2. `CreatePartition` twice, at 1 MiB (256 MiB) and 512 MiB (256 MiB). Assert that sysfs `start`/`size` match both GPT copies.
  3. `Format(p1, "ext4", "it", false)`, then `unix.Mount(p1, mnt, "ext4", 0, "")`.
  4. `DeletePartition(dev, 1)` returns `unix.EBUSY` and the table bytes are unchanged. `CheckNotBusy(p1)` returns `EBUSY`.
  5. Unmount, `DeletePartition(dev, 1)` succeeds, and the node disappears within `waitForNode`'s timeout. Use a small poll for absence.
  6. `CreatePartition` at 1 MiB again. Reading 2 MiB from its start is all zero, so the old ext4 superblock is gone.
  7. Mount p2 after `Format(p2, "xfs", "", false)` when `mkfs.xfs` exists, else ext4. `InitializeDisk` returns `EBUSY` and leaves p1 and p2 in place. Unmount, then `InitializeDisk` succeeds.

- [ ] **Step 2: Compile-check without root**

Run: `make test-go-quiet GO_TEST_PKGS=./common/btrfs/ GO_TEST_FLAGS='-run TestPartitionIntegration -v'`
Expected: PASS with `SKIP` (env var unset).

- [ ] **Step 3: Write the docs.** No stale mention of `lsblk` may remain: `rg -n lsblk docs backend --glob '!*_test.go'` returns only historical or comment-free hits.

- [ ] **Step 4: Run the full gate**

Run: `make test-quiet`
Expected: PASS. Report the exact result. Its `check-docs` step also scans `docs/superpowers/`, so every repo path named in the spec and plan must exist by now.

- [ ] **Step 5: Hand off the root test.** Ask the user to run `make test-btrfs-integration` and paste the output. Do not claim kernel-level verification until it shows `--- PASS: TestPartitionIntegration` and `--- PASS: TestBtrfsIntegration`.

- [ ] **Step 6: Suggest a commit message** to the user. Do not commit, and add no co-author trailer.
