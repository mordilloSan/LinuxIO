import { describe, expect, it } from "vitest";

import type { SwapFileRow, SwapStatus } from "@/api";
import {
  modeResultWarning,
  swapFileDone,
  swapFileTaskLabel,
  swapProgressText,
} from "@/tasks/memoryFeedback";
import {
  foreignZramConfig,
  generatorDevice,
  swapFileCreate,
  swapFileDevice,
  swapFileRow,
  swapModeResult,
  swapPartitionDevice,
  swapStatus,
  zramDevice,
} from "@/test/memoryStatusFixture";

import {
  bannerView,
  canApply,
  checkSize,
  confirmView,
  createLimits,
  createStepView,
  createView,
  type FileStepKind,
  fileStepView,
  generatorFilesNote,
  initialSelection,
  moveStepView,
  otherSwapViews,
  presetSwappiness,
  removeStepView,
  resizeLimits,
  resizeStepView,
  rowViews,
  swapFileViews,
} from "./memoryStatusView";

const GIB = 1024 ** 3;
const MIB = 1024 ** 2;

/** A host on its swap file /swap.img, which LinuxIO resizes and removes. */
const diskHost = (overrides: Parameters<typeof swapStatus>[0] = {}) =>
  swapStatus({
    devices: [swapFileDevice()],
    mode: "disk",
    recommended_mode: "zswap",
    swap_files: [swapFileRow()],
    ...overrides,
  });

/** The one row swapFileViews makes of a status with the given swap file. */
const fileView = (row: Partial<SwapFileRow>) =>
  swapFileViews(swapStatus({ swap_files: [swapFileRow(row)] }))[0];

describe("a mode the next start would not bring back", () => {
  const unsaved = diskHost({ mode: "zswap", mode_saved_for_boot: false });
  const notes = (status: ReturnType<typeof swapStatus>) =>
    rowViews(status).map((row) => [row.mode, row.unsaved]);

  it("says so on the current row only", () => {
    expect(notes(unsaved)).toEqual([
      [
        "zswap",
        "On now, but not set for the next start. Apply to keep it after a restart.",
      ],
      ["zram", undefined],
      ["disk", undefined],
    ]);
    expect(notes({ ...unsaved, mode_saved_for_boot: true })).toEqual([
      ["zswap", undefined],
      ["zram", undefined],
      ["disk", undefined],
    ]);
  });

  it("names no Apply on a row that cannot be applied", () => {
    // Another zram-generator file defines a device: zswap cannot be chosen.
    const stays = {
      ...unsaved,
      zram_generator_devices: [
        {
          device: "zram1",
          max_zram_size: "",
          path: "/etc/systemd/zram-generator.conf.d/extra.conf",
          zram_fraction: "",
          zram_size: "64",
        },
      ],
      zram_provider: "zram-generator" as const,
    };

    expect(notes(stays)[0]).toEqual([
      "zswap",
      "On now, but not set for the next start.",
    ]);
    expect(canApply(stays, "zswap")).toBe(false);
  });

  it("leaves the banner to what is parked", () => {
    expect(
      bannerView({
        ...unsaved,
        can_drain: true,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: "",
      }).action,
    ).toEqual({ blockedReason: undefined, canDrain: true, kind: "drain" });
  });
});

describe("bannerView", () => {
  it("warns that programs will be closed when nothing absorbs overflow", () => {
    expect(bannerView(swapStatus({ devices: [], mode: "none" }))).toEqual({
      action: { kind: "enable_zram" },
      severity: "warning",
      text: "If memory runs out, Linux will close programs.",
    });
  });

  it("names the tool that sets zram up in the zram row's caption", () => {
    const caption = (status: ReturnType<typeof swapStatus>) =>
      rowViews(status).find((row) => row.mode === "zram")?.technical;

    expect(caption(swapStatus())).toBe("zram (zram-tools)");
    expect(caption(swapStatus({ zram_provider: "zram-generator" }))).toBe(
      "zram (zram-generator)",
    );
    expect(
      caption(swapStatus({ zram_available: false, zram_provider: "" })),
    ).toBe("zram");
  });

  it("links to Capabilities instead when no zram tool is installed", () => {
    const view = bannerView(
      swapStatus({
        devices: [],
        mode: "none",
        zram_available: false,
        zram_provider: "",
        zram_unavailable_reason: "package_missing",
      }),
    );
    expect(view.action).toEqual({ kind: "capabilities" });
  });

  it("asks to finish a switch that left both swap kinds active", () => {
    expect(
      bannerView(
        swapStatus({
          devices: [zramDevice, swapFileDevice(GIB)],
          mode: "mixed",
          swap_files: [swapFileRow({ used: GIB })],
        }),
      ),
    ).toEqual({
      action: { kind: "none" },
      severity: "warning",
      text: "Compressed memory and a swap file are both active. Choose one below and apply to finish.",
    });
  });

  it("offers to bring parked data back when there is room", () => {
    expect(
      bannerView(
        diskHost({
          can_drain: true,
          devices: [swapFileDevice(1.5 * GIB)],
          drain_blocked_reason: "",
        }),
      ),
    ).toEqual({
      action: { blockedReason: undefined, canDrain: true, kind: "drain" },
      severity: "info",
      text: "1.5 GB of memory is parked on the system drive. Programs that need it are slower until it comes back.",
    });
  });

  it("does not count zram as parked data", () => {
    expect(
      bannerView(swapStatus({ devices: [{ ...zramDevice, used: 3 * GIB }] }))
        .text,
    ).toBe("Memory is fine. Nothing is parked.");
  });

  it.each([
    ["not_enough_memory", "Not enough free memory right now"],
    ["something_new", undefined],
  ] as const)("explains a blocked drain for %s", (code, reason) => {
    const view = bannerView(
      diskHost({
        can_drain: false,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: code as never,
      }),
    );
    expect(view.severity).toBe("info");
    expect(view.text).toMatch(/^1 GB of memory is parked/);
    expect(view.action).toEqual({
      blockedReason: reason,
      canDrain: false,
      kind: "drain",
    });
  });

  it("names the swap LinuxIO does not turn off when only that holds the data", () => {
    const view = bannerView(
      swapStatus({
        can_drain: false,
        devices: [
          swapPartitionDevice(GIB),
          swapFileDevice(GIB, "ext4", "/home/swapfile"),
          swapFileDevice(0),
        ],
        drain_blocked_reason: "other_swap",
        mode: "disk",
        swap_files: [
          swapFileRow({
            locked_reason: "not_root_only",
            path: "/home/swapfile",
            used: GIB,
          }),
          swapFileRow(),
        ],
      }),
    );
    expect(view.action).toEqual({
      blockedReason:
        "The data is on /dev/vdb1 and /home/swapfile, which LinuxIO does not turn off, so it stays there",
      canDrain: false,
      kind: "drain",
    });
  });

  it("says that a btrfs swap file blocks snapshots and offers the switch", () => {
    expect(
      bannerView(
        diskHost({
          devices: [swapFileDevice(0, "btrfs")],
          swap_files: [
            swapFileRow({ blocks_snapshots: true, fs_type: "btrfs" }),
          ],
        }),
      ),
    ).toEqual({
      action: { kind: "switch_zram" },
      severity: "warning",
      text: "/swap.img blocks snapshots of the subvolume it is in.",
    });
  });

  it("names every swap file that blocks snapshots", () => {
    const blocking = (path: string) =>
      swapFileRow({ blocks_snapshots: true, fs_type: "btrfs", path });
    expect(
      bannerView(
        diskHost({
          swap_files: [blocking("/swap.img"), blocking("/swap2.img")],
        }),
      ).text,
    ).toBe(
      "/swap.img and /swap2.img block snapshots of the subvolumes they are in.",
    );
  });

  it("hides the switch when zram cannot be chosen", () => {
    const view = bannerView(
      diskHost({
        swap_files: [swapFileRow({ blocks_snapshots: true })],
        zram_available: false,
        zram_provider: "",
        zram_unavailable_reason: "package_missing",
      }),
    );
    expect(view.action).toEqual({ kind: "none" });
  });

  it("offers no switch when it would not remove the blocking file", () => {
    expect(
      bannerView(
        diskHost({
          swap_files: [
            swapFileRow({
              blocks_snapshots: true,
              locked_reason: "not_in_fstab",
            }),
          ],
        }),
      ).action,
    ).toEqual({ kind: "none" });
  });

  it("ignores a btrfs swap file that is off", () => {
    expect(
      bannerView(
        swapStatus({
          swap_files: [swapFileRow({ active: false, blocks_snapshots: true })],
        }),
      ).severity,
    ).toBe("success");
  });

  it("reports a healthy host", () => {
    expect(bannerView(swapStatus())).toEqual({
      action: { kind: "none" },
      severity: "success",
      text: "Memory is fine. Nothing is parked.",
    });
  });
});

describe("rowViews", () => {
  it("lists zswap, zram and disk in that order with chips", () => {
    const rows = rowViews(diskHost());
    expect(rows.map((row) => row.mode)).toEqual(["zswap", "zram", "disk"]);
    expect(rows.map((row) => row.recommended)).toEqual([true, false, false]);
    expect(rows.map((row) => row.current)).toEqual([false, false, true]);
    expect(rows.map((row) => row.name)).toEqual([
      "Compressed memory with the disk as backup",
      "Compressed memory only",
      "Disk only",
    ]);
  });

  it("puts no Current chip on a mode that is not a row", () => {
    const rows = rowViews(swapStatus({ devices: [], mode: "none" }));
    expect(rows.some((row) => row.current)).toBe(false);
  });

  it("sizes zram from the planned size and a third of it", () => {
    const zram = rowViews(swapStatus({ zram_planned_size: 6 * GIB })).find(
      (row) => row.mode === "zram",
    );
    expect(zram?.description).toContain(
      "Holds about 6 GB using about 2 GB of memory.",
    );
  });

  it("offers zswap and disk with no disk swap, creating LinuxIO's swap file first", () => {
    const rows = rowViews(swapStatus());
    const note =
      "Creates the swap file /var/lib/linuxio/swap/swapfile first, at a size you choose.";
    expect(rows[0].disabledReason).toBeUndefined();
    expect(rows[2].disabledReason).toBeUndefined();
    expect(rows[0].note).toBe(note);
    expect(rows[2].note).toBe(note);
    expect(rows[1].note).toBeUndefined();
    expect(canApply(swapStatus(), "disk")).toBe(true);
  });

  it("says why zswap and disk cannot be chosen when LinuxIO cannot create its swap file", () => {
    const rows = rowViews(
      swapStatus({
        create: swapFileCreate({ blocked_reason: "read_only" }),
      }),
    );
    const reason =
      "Needs a swap file, and LinuxIO cannot create one: the filesystem of /var/lib/linuxio/swap is read-only.";
    expect(rows[0].disabledReason).toBe(reason);
    expect(rows[2].disabledReason).toBe(reason);
    expect(rows[0].note).toBeUndefined();
    expect(rows[1].disabledReason).toBeUndefined();
  });

  it("says a swap file in /etc/fstab that is off is not turned on", () => {
    const rows = rowViews(
      swapStatus({
        swap_files: [
          swapFileRow({ active: false, resize_blocked_reason: "off" }),
        ],
      }),
    );
    expect(rows[2].disabledReason).toBe(
      "/swap.img is in /etc/fstab and off. LinuxIO does not turn it on: remove it under Swap files to choose this.",
    );
    expect(rows[0].disabledReason).toBe(rows[2].disabledReason);
    const locked = rowViews(
      swapStatus({
        swap_files: [
          swapFileRow({
            active: false,
            locked_reason: "not_root_only",
            resize_blocked_reason: "off",
          }),
        ],
      }),
    );
    expect(locked[2].disabledReason).toBe(
      "/swap.img is in /etc/fstab and off, and LinuxIO does not turn it on.",
    );
  });

  it("says a swap file in /etc/fstab that is missing can be removed to choose zswap or disk", () => {
    const missing = swapFileRow({
      active: false,
      resize_blocked_reason: "missing",
      size: 0,
    });
    const rows = rowViews(swapStatus({ swap_files: [missing] }));
    expect(rows[0].disabledReason).toBe(
      "/etc/fstab names /swap.img, and nothing is at its path. Remove it under Swap files to choose this.",
    );
    expect(rows[2].disabledReason).toBe(rows[0].disabledReason);
    expect(
      rowViews(
        swapStatus({
          swap_files: [{ ...missing, locked_reason: "not_root_only" }],
        }),
      )[2].disabledReason,
    ).toBe("/etc/fstab names /swap.img, and nothing is at its path.");
  });

  it("keeps zswap and disk while any disk swap is on, a partition included", () => {
    for (const device of [swapFileDevice(), swapPartitionDevice()]) {
      const rows = rowViews(swapStatus({ devices: [device], mode: "disk" }));
      expect(rows[0].disabledReason).toBeUndefined();
      expect(rows[2].disabledReason).toBeUndefined();
      expect(rows[0].note).toBeUndefined();
    }
  });

  it("offers zram beside a swap partition and says the partition stays on", () => {
    const rows = rowViews(
      swapStatus({ devices: [swapPartitionDevice()], mode: "disk" }),
    );
    expect(rows[1].disabledReason).toBeUndefined();
    expect(rows[1].note).toBe(
      "/dev/vdb1 stays on and takes what compressed memory cannot hold.",
    );
    expect(
      canApply(
        swapStatus({ devices: [swapPartitionDevice()], mode: "disk" }),
        "zram",
      ),
    ).toBe(true);
  });

  it("disables zswap on a kernel without it", () => {
    const rows = rowViews(
      diskHost({
        zswap: {
          available: false,
          compressor: "",
          enabled: false,
          max_pool_percent: 0,
        },
      }),
    );
    expect(rows[0].disabledReason).toBe("This kernel has no zswap");
  });

  it("says on zswap and disk why leaving zram is refused while another zram-generator file defines a device", () => {
    const status = swapStatus({
      zram_generator_devices: [generatorDevice()],
      zram_provider: "zram-generator",
    });
    const reason =
      "/etc/systemd/zram-generator.conf.d/50-big.conf turns compressed memory on at every start. LinuxIO does not change that file. To choose this, remove that file or take the device out of it.";
    const rows = rowViews(status);
    expect(rows[0].disabledReason).toBe(reason);
    expect(rows[2].disabledReason).toBe(reason);
    expect(rows[1].disabledReason).toBeUndefined();
    // A row that cannot be chosen does not say what choosing it would do.
    expect(rows[2].note).toBeUndefined();
    expect(canApply(status, "disk")).toBe(false);
    // zram-tools does not read those files.
    expect(
      rowViews({ ...status, zram_provider: "zram-tools" })[2].disabledReason,
    ).toBeUndefined();
  });

  it("says how to turn a package's zram-generator file off", () => {
    const rows = rowViews(
      swapStatus({
        zram_generator_devices: [
          generatorDevice({
            path: "/usr/lib/systemd/zram-generator.conf.d/50-big.conf",
          }),
        ],
        zram_provider: "zram-generator",
      }),
    );
    expect(rows[2].disabledReason).toBe(
      "/usr/lib/systemd/zram-generator.conf.d/50-big.conf turns compressed memory on at every start. LinuxIO does not change that file. To turn it off, link /etc/systemd/zram-generator.conf.d/50-big.conf to /dev/null.",
    );
  });

  it("replaces the zram radio with a Capabilities link when the package is missing", () => {
    const zram = rowViews(
      swapStatus({
        zram_available: false,
        zram_provider: "",
        zram_unavailable_reason: "package_missing",
      }),
    )[1];
    expect(zram.capabilitiesLink).toBe(true);
    expect(zram.disabledReason).toBeUndefined();
  });

  it("carries the cautions", () => {
    const rows = rowViews(diskHost());
    expect(rows[0].caution).toBeUndefined();
    expect(rows[1].caution).toBe(
      "No backup. If it fills up, Linux closes the hungriest program, and a VM would stop like a power cut.",
    );
  });

  it("speaks of snapshots only while an active swap file blocks them", () => {
    const zramText = (status: ReturnType<typeof swapStatus>) =>
      rowViews(status)[1].description;
    const blocking = swapFileRow({ blocks_snapshots: true, fs_type: "btrfs" });

    // A swap file on ext4 or in LinuxIO's own subvolume blocks none.
    const plain = rowViews(diskHost());
    expect(plain[2].caution).toBeUndefined();
    expect(zramText(diskHost())).toBe(
      "The same, with no swap file. Writes nothing to disk. Holds about 8 GB using about 2.67 GB of memory.",
    );

    const blocked = rowViews(diskHost({ swap_files: [blocking] }));
    expect(blocked[2].caution).toBe(
      "Blocks snapshots of the system drive while in use.",
    );
    expect(zramText(diskHost({ swap_files: [blocking] }))).toBe(
      "The same, with no swap file. Writes nothing to disk and snapshots of the system drive work. Holds about 8 GB using about 2.67 GB of memory.",
    );

    // A blocking file LinuxIO does not remove stays on beside compressed
    // memory, so the row promises nothing about snapshots.
    const locked = diskHost({
      swap_files: [
        blocking,
        { ...blocking, locked_reason: "not_in_fstab", path: "/swapfile" },
      ],
    });
    expect(rowViews(locked)[2].caution).toBe(
      "Blocks snapshots of the system drive while in use.",
    );
    expect(zramText(locked)).toMatch(
      /^The same, with no swap file\. Writes nothing to disk\. Holds/,
    );

    // A file that is off blocks nothing.
    const off = diskHost({ swap_files: [{ ...blocking, active: false }] });
    expect(rowViews(off)[2].caution).toBeUndefined();
    expect(zramText(off)).toMatch(
      /^The same, with no swap file\. Writes nothing to disk\. Holds/,
    );
  });
});

describe("swapFileViews", () => {
  it("shows an active file's size and use, with Resize and Remove", () => {
    const view = fileView({ used: 1.5 * GIB });
    expect(view.path).toBe("/swap.img");
    expect(view.summary).toBe("4 GB, 1.5 GB used");
    expect(view.notes).toEqual([]);
    expect(view.resize).toBe(true);
    expect(view.remove).toBe(true);
    expect(view.move).toBe(false);
    expect(view.snapshots).toBeUndefined();
    expect(fileView({}).summary).toBe("4 GB, none used");
  });

  it.each([
    [
      "not_in_fstab",
      "LinuxIO does not resize or remove it: something other than /etc/fstab turns it on, so LinuxIO cannot keep it off.",
    ],
    [
      "not_root_only",
      "LinuxIO does not resize or remove it: another user could put a file of their own at this name.",
    ],
    [
      "hibernation",
      "LinuxIO does not resize or remove it: changing this file would break hibernation.",
    ],
    ["something_new", "LinuxIO does not resize or remove it."],
  ] as const)("says why a %s file has no buttons", (reason, text) => {
    const view = fileView({
      can_move: true,
      locked_reason: reason as never,
    });
    expect(view.notes).toEqual([text]);
    expect(view.resize).toBe(false);
    expect(view.remove).toBe(false);
    expect(view.move).toBe(false);
  });

  it.each([
    [
      "off",
      "Off, 4 GB",
      ["/etc/fstab names it, and LinuxIO does not turn it on."],
    ],
    [
      "missing",
      "Missing",
      [
        "/etc/fstab names it, and nothing is at its path. Remove takes its line out of /etc/fstab.",
      ],
    ],
    [
      "no_swap_header",
      "Not a swap file, 4 GB",
      ["It is off and holds no swap signature."],
    ],
  ] as const)(
    "reads %s for a file that is off, with Remove only",
    (reason, summary, notes) => {
      const view = fileView({ active: false, resize_blocked_reason: reason });
      expect(view.summary).toBe(summary);
      expect(view.notes).toEqual(notes);
      expect(view.resize).toBe(false);
      expect(view.remove).toBe(true);
    },
  );

  it.each([
    [
      "unsupported_filesystem",
      "It cannot be resized: it is on ext4, and LinuxIO resizes swap files only on ext4, xfs and btrfs.",
    ],
    ["read_only", "It cannot be resized: its filesystem is read-only."],
    [
      "btrfs_layout",
      "It cannot be resized: its btrfs filesystem has more than one device or data that is not single.",
    ],
  ] as const)(
    "says why an active file cannot be resized for %s",
    (reason, text) => {
      const view = fileView({ resize_blocked_reason: reason });
      expect(view.summary).toBe("4 GB, none used");
      expect(view.notes).toEqual([text]);
      expect(view.resize).toBe(false);
      expect(view.remove).toBe(true);
    },
  );

  it("says what a temporary file is and offers Remove only", () => {
    const view = fileView({
      path: "/swap.img.linuxio-resize",
      temporary: true,
    });
    expect(view.notes).toEqual([
      "A temporary swap file a resize left on. Remove turns it off and deletes it.",
    ]);
    expect(view.resize).toBe(false);
    expect(view.remove).toBe(true);
  });

  it("puts the btrfs line on the file it is about, with Move only when offered", () => {
    const blocking = fileView({ blocks_snapshots: true, fs_type: "btrfs" });
    expect(blocking.snapshots).toBe(
      "/swap.img blocks snapshots of the subvolume it is in.",
    );
    expect(blocking.move).toBe(false);
    // A file that is off blocks nothing.
    expect(
      fileView({
        active: false,
        blocks_snapshots: true,
        fs_type: "btrfs",
        resize_blocked_reason: "off",
      }).snapshots,
    ).toBeUndefined();
    const movable = fileView({
      blocks_snapshots: true,
      can_move: true,
      fs_type: "btrfs",
    });
    expect(movable.snapshots).toBe(
      "/swap.img blocks snapshots of the subvolume it is in. LinuxIO can make it again in a subvolume of its own, at the same size.",
    );
    expect(movable.move).toBe(true);
  });
});

describe("otherSwapViews", () => {
  it("lists swap partitions and zram as plain rows, and no swap files", () => {
    expect(
      otherSwapViews(
        swapStatus({
          devices: [
            { ...zramDevice, used: GIB },
            swapPartitionDevice(),
            swapFileDevice(),
          ],
        }),
      ),
    ).toEqual([
      {
        kind: "Compressed memory",
        path: "/dev/zram0",
        summary: "8 GB, 1 GB used",
      },
      { kind: "Partition", path: "/dev/vdb1", summary: "2 GB, none used" },
    ]);
  });
});

describe("createView", () => {
  it("offers Create from the default size up to what the disk allows", () => {
    expect(createView(swapStatus())).toEqual({
      kind: "offer",
      size: { max: 38 * 1024, min: 256, start: 8 * 1024 },
    });
  });

  it("says nothing while LinuxIO's swap file is there", () => {
    expect(
      createView(
        swapStatus({ create: swapFileCreate({ blocked_reason: "exists" }) }),
      ),
    ).toEqual({ kind: "none" });
  });

  it.each([
    [
      "unsupported_filesystem",
      "LinuxIO cannot create a swap file: /var/lib/linuxio/swap is on ext4, and LinuxIO makes swap files only on ext4, xfs and btrfs.",
    ],
    [
      "read_only",
      "LinuxIO cannot create a swap file: the filesystem of /var/lib/linuxio/swap is read-only.",
    ],
    [
      "btrfs_layout",
      "LinuxIO cannot create a swap file: the btrfs filesystem of /var/lib/linuxio/swap has more than one device or data that is not single.",
    ],
    [
      "not_enough_space",
      "LinuxIO cannot create a swap file: the disk has 40 GB free, and a swap file of at least 256 MB must still leave 2 GB free.",
    ],
    [
      "folder_not_root_only",
      "LinuxIO cannot create a swap file: a folder on /var/lib/linuxio/swap/swapfile is a link or can be changed by users other than root.",
    ],
    [
      "path_taken",
      "LinuxIO cannot create a swap file: something other than its swap file is in /var/lib/linuxio/swap.",
    ],
    [
      "hibernation",
      "LinuxIO cannot create a swap file: the host hibernates to a swap file on the disk /var/lib/linuxio/swap is on, where LinuxIO changes no swap file.",
    ],
    [
      "something_new",
      "LinuxIO cannot create a swap file: LinuxIO cannot use its folder.",
    ],
  ] as const)("says why it cannot for %s", (reason, text) => {
    expect(
      createView(
        swapStatus({
          create: swapFileCreate({ blocked_reason: reason as never }),
        }),
      ),
    ).toEqual({ kind: "blocked", reason: text });
  });
});

describe("size fields", () => {
  it("starts Create at the default size, kept within the limits", () => {
    expect(createLimits(swapStatus())).toEqual({
      max: 38 * 1024,
      min: 256,
      start: 8 * 1024,
    });
    expect(
      createLimits(
        swapStatus({ create: swapFileCreate({ max_size: 3 * GIB }) }),
      ).start,
    ).toBe(3 * 1024);
  });

  it("starts a resize at the file's size in whole MiB, up to its own limit", () => {
    // A swap file reports one page less than its length.
    const row = swapFileRow({ max_size: 30 * GIB, size: 4 * GIB - 4096 });
    expect(resizeLimits(swapStatus(), row)).toEqual({
      max: 30 * 1024,
      min: 256,
      start: 4 * 1024,
    });
  });

  it("takes the smallest size as 256 MiB when the status gives none", () => {
    expect(
      createLimits(swapStatus({ create: swapFileCreate({ min_size: 0 }) })).min,
    ).toBe(256);
  });

  const field = { max: 1024, min: 256, start: 512 };
  const help =
    "From 256 to 1024 MiB. 1024 MiB is the largest size the disk allows.";

  it("accepts whole MiB from the smallest to the largest size", () => {
    expect(checkSize("256", field)).toEqual({ bytes: 256 * MIB, help });
    expect(checkSize(" 1024 ", field)).toEqual({ bytes: 1024 * MIB, help });
  });

  it.each([
    ["", "Enter a whole number of MiB."],
    ["1.5", "Enter a whole number of MiB."],
    ["-1", "Enter a whole number of MiB."],
    ["255", "At least 256 MiB."],
    ["1025", "At most 1024 MiB, the largest size the disk allows."],
  ])("refuses %j", (text, error) => {
    expect(checkSize(text, field)).toEqual({ error, help });
  });

  it("says when the disk has no room for the smallest file", () => {
    expect(checkSize("256", { max: 100, min: 256, start: 256 })).toEqual({
      error: "The disk has no room for a swap file of 256 MiB.",
      help: "",
    });
  });
});

describe("initialSelection", () => {
  it("starts on the current mode when it is a row", () => {
    expect(
      initialSelection(swapStatus({ mode: "disk", recommended_mode: "zram" })),
    ).toBe("disk");
  });

  it.each(["none", "mixed"] as const)(
    "starts on the recommended row when the mode is %s",
    (mode) => {
      expect(
        initialSelection(swapStatus({ mode, recommended_mode: "zswap" })),
      ).toBe("zswap");
    },
  );
});

describe("canApply", () => {
  it("is off while the selection is already the mode", () => {
    expect(canApply(swapStatus({ mode: "zram" }), "zram")).toBe(false);
  });

  it("is on for a different row that can be chosen", () => {
    expect(canApply(diskHost(), "zram")).toBe(true);
  });

  it("is on for the same row while the mode is mixed", () => {
    expect(
      canApply(
        swapStatus({ devices: [zramDevice, swapFileDevice()], mode: "mixed" }),
        "zram",
      ),
    ).toBe(true);
  });

  it("is off for a row that is disabled", () => {
    expect(
      canApply(
        swapStatus({ create: swapFileCreate({ blocked_reason: "read_only" }) }),
        "disk",
      ),
    ).toBe(false);
  });

  // zswap turned on by hand: the mode reads zswap, and a restart loses it.
  it("is on for the current row while the next start would not bring it back", () => {
    const status = diskHost({ mode: "zswap", mode_saved_for_boot: false });

    expect(canApply(status, "zswap")).toBe(true);
    expect(canApply({ ...status, mode_saved_for_boot: true }, "zswap")).toBe(
      false,
    );
  });
});

describe("confirmView", () => {
  const replaced = (path: string) =>
    `${path} holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as ${path}.before-linuxio.`;
  const BROUGHT_BACK =
    "Data parked in a swap file is brought back to memory before the file is turned off and deleted.";

  it("lists every swap file a switch to zram removes, with its size, and sends their paths", () => {
    const status = diskHost({
      devices: [swapFileDevice(), swapFileDevice(0, "ext4", "/swap2.img")],
      swap_files: [
        swapFileRow(),
        swapFileRow({ path: "/swap2.img", size: 20 * GIB }),
        swapFileRow({ locked_reason: "not_in_fstab", path: "/var/swap" }),
        swapFileRow({
          active: false,
          path: "/old.img",
          resize_blocked_reason: "missing",
          size: 0,
        }),
      ],
    });
    expect(confirmView(status, "zram")).toEqual({
      action: "Turn on compressed memory",
      items: [
        "Compressed memory is turned on.",
        "/swap.img, 4 GB, is removed.",
        "/swap2.img, 20 GB, is removed.",
        "Nothing is at /old.img. Its line is taken out of /etc/fstab.",
        BROUGHT_BACK,
        "/var/swap stays on and takes what compressed memory cannot hold.",
      ],
      remove: ["/swap.img", "/swap2.img", "/old.img"],
      title: "Switch to compressed memory?",
    });
    expect(confirmView(status, "zswap")).toBeUndefined();
  });

  it("says data is brought back only when a removed file swaps", () => {
    const status = swapStatus({
      swap_files: [
        swapFileRow({ active: false, resize_blocked_reason: "off" }),
        swapFileRow({
          active: false,
          path: "/old.img",
          resize_blocked_reason: "missing",
          size: 0,
        }),
      ],
    });
    expect(confirmView(status, "zram")).toEqual({
      action: "Turn on compressed memory",
      items: [
        "Compressed memory is turned on.",
        "/swap.img, 4 GB, is removed.",
        "Nothing is at /old.img. Its line is taken out of /etc/fstab.",
      ],
      remove: ["/swap.img", "/old.img"],
      title: "Switch to compressed memory?",
    });
  });

  it("says a swap partition stays on beside compressed memory", () => {
    const status = diskHost({
      devices: [swapFileDevice(), swapPartitionDevice()],
    });
    expect(confirmView(status, "zram")?.items).toEqual([
      "Compressed memory is turned on.",
      "/swap.img, 4 GB, is removed.",
      BROUGHT_BACK,
      "/dev/vdb1 stays on and takes what compressed memory cannot hold.",
    ]);
  });

  it("does not ask when a switch to zram removes nothing", () => {
    expect(confirmView(swapStatus(), "zram")).toBeUndefined();
    expect(
      confirmView(
        diskHost({
          swap_files: [swapFileRow({ locked_reason: "hibernation" })],
        }),
        "zram",
      ),
    ).toBeUndefined();
    expect(
      confirmView(
        swapStatus({ devices: [swapPartitionDevice()], mode: "disk" }),
        "zram",
      ),
    ).toBeUndefined();
  });

  it("sets the size of the swap file a switch to disk or zswap creates", () => {
    const create = {
      action: "Apply",
      items: [
        "LinuxIO creates the swap file /var/lib/linuxio/swap/swapfile, adds it to /etc/fstab and turns it on.",
      ],
      size: { max: 38 * 1024, min: 256, start: 8 * 1024 },
      title: "Switch to disk only?",
    };
    expect(confirmView(swapStatus(), "disk")).toEqual(create);
    expect(confirmView(swapStatus(), "zswap")).toEqual({
      ...create,
      title: "Switch to compressed memory with the disk as backup?",
    });
    // With disk swap on, nothing is created.
    expect(confirmView(diskHost({ mode: "zswap" }), "disk")).toBeUndefined();
  });

  it("names a zram configuration file that is not LinuxIO's and where its copy goes", () => {
    const status = swapStatus({
      devices: [],
      foreign_zram_config: foreignZramConfig(),
      mode: "none",
    });
    expect(confirmView(status, "zram")).toEqual({
      action: "Turn on compressed memory",
      items: [
        "Compressed memory is turned on.",
        replaced("/etc/default/zramswap"),
      ],
      replace: "/etc/default/zramswap",
      title: "Switch to compressed memory?",
    });
  });

  it("lists everything that needs confirming in one step", () => {
    const status = diskHost({ foreign_zram_config: foreignZramConfig() });
    expect(confirmView(status, "zram")).toMatchObject({
      items: [
        "Compressed memory is turned on.",
        "/swap.img, 4 GB, is removed.",
        BROUGHT_BACK,
        replaced("/etc/default/zramswap"),
      ],
      remove: ["/swap.img"],
      replace: "/etc/default/zramswap",
    });
  });

  it("asks before leaving zram only when zram-generator's file is replaced then", () => {
    const generator = swapStatus({
      devices: [zramDevice, swapPartitionDevice()],
      foreign_zram_config: foreignZramConfig(
        "/etc/systemd/zram-generator.conf",
      ),
      zram_provider: "zram-generator",
    });
    expect(confirmView(generator, "disk")).toEqual({
      action: "Apply",
      items: [replaced("/etc/systemd/zram-generator.conf")],
      replace: "/etc/systemd/zram-generator.conf",
      title: "Switch to disk only?",
    });
    // zram-tools only disables its service on the way out.
    expect(
      confirmView({ ...generator, zram_provider: "zram-tools" }, "disk"),
    ).toBeUndefined();
  });
});

describe("swap file steps", () => {
  it("creates LinuxIO's swap file at a size the user sets", () => {
    expect(createStepView(swapStatus())).toEqual({
      action: "Create",
      items: [
        "LinuxIO creates the swap file /var/lib/linuxio/swap/swapfile, adds it to /etc/fstab and turns it on.",
      ],
      size: { max: 38 * 1024, min: 256, start: 8 * 1024 },
      title: "Create a swap file",
    });
  });

  it("resizes from the current size and says the file is turned off and on again", () => {
    expect(resizeStepView(diskHost(), swapFileRow())).toEqual({
      action: "Resize",
      items: [
        "/swap.img is 4 GB.",
        "To change its size, LinuxIO turns the file off and on again. Data parked in it is brought back to memory first.",
      ],
      sameSize: 4 * 1024,
      size: { max: 30 * 1024, min: 256, start: 4 * 1024 },
      title: "Resize /swap.img",
    });
  });

  it("names the file and its size before removing it", () => {
    expect(
      removeStepView(
        diskHost({ devices: [zramDevice, swapFileDevice()] }),
        swapFileRow(),
      ),
    ).toEqual({
      action: "Remove",
      items: [
        "/swap.img, 4 GB, is turned off, taken out of /etc/fstab and deleted.",
        "Data parked in it is brought back to memory first.",
      ],
      title: "Remove /swap.img?",
    });
  });

  it("warns that removing the last swap leaves the host without any", () => {
    expect(removeStepView(diskHost(), swapFileRow()).items).toEqual([
      "/swap.img, 4 GB, is turned off, taken out of /etc/fstab and deleted.",
      "Data parked in it is brought back to memory first.",
      "The host then has no swap. If memory runs out, Linux will close programs.",
    ]);
  });

  it("says what Remove does to a file that is off, missing or temporary", () => {
    const status = swapStatus();
    expect(
      removeStepView(
        status,
        swapFileRow({ active: false, resize_blocked_reason: "off" }),
      ).items,
    ).toEqual(["/swap.img, 4 GB, is taken out of /etc/fstab and deleted."]);
    expect(
      removeStepView(
        status,
        swapFileRow({ active: false, resize_blocked_reason: "missing" }),
      ).items,
    ).toEqual([
      "Nothing is at /swap.img. Its line is taken out of /etc/fstab.",
    ]);
    expect(
      removeStepView(
        swapStatus({ devices: [zramDevice, swapFileDevice()] }),
        swapFileRow({
          in_fstab: false,
          path: "/swap.img.linuxio-resize",
          size: GIB,
          temporary: true,
        }),
      ).items,
    ).toEqual([
      "The temporary swap file /swap.img.linuxio-resize, 1 GB, is turned off and deleted.",
      "Data parked in it is brought back to memory first.",
    ]);
  });

  it("says a move creates LinuxIO's file before it removes the old one", () => {
    expect(moveStepView(diskHost(), swapFileRow())).toEqual({
      action: "Move",
      items: [
        "LinuxIO creates the swap file /var/lib/linuxio/swap/swapfile, 4 GB, in a subvolume of its own, adds it to /etc/fstab and turns it on.",
        "Then /swap.img is turned off, taken out of /etc/fstab and deleted. Data parked in it is brought back to memory first.",
      ],
      title: "Move /swap.img?",
    });
    expect(
      moveStepView(
        swapStatus(),
        swapFileRow({ active: false, resize_blocked_reason: "off" }),
      ).items[1],
    ).toBe("Then /swap.img is taken out of /etc/fstab and deleted.");
  });

  it("builds a swap file step from the status as it is now", () => {
    expect(fileStepView(diskHost(), "resize", "/swap.img")).toEqual(
      resizeStepView(diskHost(), swapFileRow()),
    );
    expect(fileStepView(diskHost(), "remove", "/swap.img")).toEqual(
      removeStepView(diskHost(), swapFileRow()),
    );
    expect(fileStepView(swapStatus(), "create", "")).toEqual(
      createStepView(swapStatus()),
    );
    const movable = swapFileRow({ blocks_snapshots: true, can_move: true });
    expect(
      fileStepView(diskHost({ swap_files: [movable] }), "move", "/swap.img"),
    ).toEqual(moveStepView(diskHost(), movable));
  });

  it.each([
    ["resize", "gone", { swap_files: [] }],
    [
      "resize",
      "no longer resizable",
      { swap_files: [swapFileRow({ resize_blocked_reason: "read_only" })] },
    ],
    [
      "remove",
      "locked",
      { swap_files: [swapFileRow({ locked_reason: "hibernation" })] },
    ],
    ["move", "no longer movable", { swap_files: [swapFileRow()] }],
  ] as [FileStepKind, string, Partial<SwapStatus>][])(
    "says a %s step's file changed when it is %s",
    (kind, _how, overrides) => {
      const view = fileStepView(diskHost(overrides), kind, "/swap.img");
      expect(view.changed).toBe(true);
      expect(view.size).toBeUndefined();
      expect(view.items).toEqual([
        "/swap.img changed while this was open, so go back to see what can be done with it now.",
      ]);
    },
  );

  it("says Create can no longer be done when LinuxIO's path is taken", () => {
    expect(
      fileStepView(
        swapStatus({ create: swapFileCreate({ blocked_reason: "exists" }) }),
        "create",
        "",
      ),
    ).toEqual({
      action: "Create",
      changed: true,
      items: [
        "LinuxIO can no longer create a swap file here, so go back to see why.",
      ],
      title: "Create a swap file",
    });
  });
});

describe("generatorFilesNote", () => {
  it("says nothing when no other zram-generator file defines a device", () => {
    expect(generatorFilesNote(swapStatus())).toBeUndefined();
  });

  it("lists each file with the device and the size settings it writes", () => {
    expect(
      generatorFilesNote(
        swapStatus({
          zram_generator_devices: [
            generatorDevice({
              device: "zram0",
              path: "/run/systemd/zram-generator.conf",
              zram_size: "ram / 8",
            }),
            generatorDevice({
              max_zram_size: "2048",
              zram_fraction: "0.25",
              zram_size: "",
            }),
            generatorDevice({ device: "zram2", zram_size: "" }),
          ],
          zram_provider: "zram-generator",
        }),
      ),
    ).toEqual({
      files: [
        "/run/systemd/zram-generator.conf sets up zram0 with zram-size = ram / 8",
        "/etc/systemd/zram-generator.conf.d/50-big.conf sets up zram1 with zram-fraction = 0.25, max-zram-size = 2048",
        "/etc/systemd/zram-generator.conf.d/50-big.conf sets up zram2 and sets no size",
      ],
      text: "zram-generator also reads these files, and what they set wins over the settings LinuxIO writes:",
    });
  });

  it("does not say the files win while zram-tools runs zram", () => {
    expect(
      generatorFilesNote(
        swapStatus({ zram_generator_devices: [generatorDevice()] }),
      )?.text,
    ).toBe(
      "zram-generator is installed too and sets up compressed memory from these files:",
    );
  });
});

describe("presetSwappiness", () => {
  it("reads the preset of a mode", () => {
    expect(presetSwappiness(swapStatus(), "zswap")).toBe(60);
    expect(presetSwappiness(swapStatus(), "zram")).toBe(180);
  });
});

describe("modeResultWarning", () => {
  it("is null when the host reached the mode and nothing was left", () => {
    expect(
      modeResultWarning(swapModeResult(swapStatus({ mode: "zram" })), "zram"),
    ).toBeNull();
  });

  it.each([
    [
      "swap_file",
      "not_enough_memory",
      "Not enough free memory to bring back the data in /swap.img, so it stays on. Apply again when more memory is free.",
    ],
    [
      "swap_file",
      "zram_not_swapping",
      "Compressed memory is not running, so /swap.img stays on.",
    ],
    [
      "zram",
      "not_managed",
      "Compressed memory is still on: it is not run by a zram tool that LinuxIO manages.",
    ],
    [
      "zram_size",
      "other_size",
      "Compressed memory came up at another size than planned. Check the zram tool's configuration.",
    ],
    [
      "zram_size",
      "not_enough_memory",
      "Compressed memory keeps its current size: not enough free memory to restart it. Apply again when more memory is free.",
    ],
  ] as const)("says why %s was left for %s", (what, reason, text) => {
    const path = what === "swap_file" ? "/swap.img" : undefined;
    expect(
      modeResultWarning(
        swapModeResult(swapStatus({ mode: "mixed" }), [{ path, reason, what }]),
        "zram",
      ),
    ).toBe(text);
  });

  it("gives every reason when more than one thing was left", () => {
    expect(
      modeResultWarning(
        swapModeResult(swapStatus({ mode: "mixed" }), [
          { reason: "not_managed", what: "zram_size" },
          { path: "/swap.img", reason: "zram_not_swapping", what: "swap_file" },
          {
            path: "/swap2.img",
            reason: "not_enough_memory",
            what: "swap_file",
          },
        ]),
        "zram",
      ),
    ).toBe(
      "Compressed memory keeps its current size: it is not run by a zram tool that LinuxIO manages. Compressed memory is not running, so /swap.img stays on. Not enough free memory to bring back the data in /swap2.img, so it stays on. Apply again when more memory is free.",
    );
  });

  it("says the swap file when the result does not name it", () => {
    expect(
      modeResultWarning(
        swapModeResult(swapStatus({ mode: "mixed" }), [
          { reason: "zram_not_swapping", what: "swap_file" },
        ]),
        "zram",
      ),
    ).toBe("Compressed memory is not running, so the swap file stays on.");
  });

  it("still warns for a pair it has no text for, and for another mode with no reason", () => {
    expect(
      modeResultWarning(
        swapModeResult(swapStatus({ mode: "mixed" }), [
          { reason: "no_other_swap", what: "swap_file" },
        ]),
        "zram",
      ),
    ).toBe("Part of the change was not applied.");
    expect(
      modeResultWarning(swapModeResult(swapStatus({ mode: "disk" })), "zram"),
    ).toBe("The host did not reach this setting. Apply again.");
  });
});

describe("swap file job feedback", () => {
  it.each([
    ["create", "Swap file created", "Failed to create the swap file"],
    ["resize", "Resized /swap.img", "Failed to resize /swap.img"],
    ["remove", "Removed /swap.img", "Failed to remove /swap.img"],
    [
      "move",
      "Moved /swap.img into LinuxIO's folder",
      "Failed to move /swap.img",
    ],
  ] as const)("reports a %s job's end", (action, succeeded, failed) => {
    expect(swapFileDone(action, "/swap.img")).toEqual({ failed, succeeded });
  });

  it("says the swap file when the job does not name it", () => {
    expect(swapFileDone("remove", "")).toEqual(
      swapFileDone("remove", undefined),
    );
    expect(swapFileDone("remove", undefined)).toEqual({
      failed: "Failed to remove the swap file",
      succeeded: "Removed the swap file",
    });
  });

  it.each([
    ["create", "Creating a swap file"],
    ["resize", "Resizing /swap.img"],
    ["remove", "Removing /swap.img"],
    ["move", "Moving /swap.img"],
  ] as const)("names a %s job in the task tray", (action, label) => {
    expect(swapFileTaskLabel(action, "/swap.img")).toBe(label);
  });

  it.each([
    [
      "create_swap_file",
      "Creating the swap file /var/lib/linuxio/swap/swapfile",
    ],
    [
      "make_temporary_swap",
      "Adding the temporary swap file /var/lib/linuxio/swap/swapfile",
    ],
    ["resize_swap_file", "Resizing /var/lib/linuxio/swap/swapfile"],
    [
      "remove_temporary_swap",
      "Removing the temporary swap file /var/lib/linuxio/swap/swapfile",
    ],
    ["remove_swap_file", "Removing /var/lib/linuxio/swap/swapfile"],
  ] as const)("names the file the %s step acts on", (step, text) => {
    expect(
      swapProgressText({
        detail: {
          device: "/var/lib/linuxio/swap/swapfile",
          index: 1,
          step,
          total: 3,
        },
        message: "Backend message",
      }),
    ).toBe(text);
  });

  it("shows the backend's message for any other step, or none before the first", () => {
    expect(
      swapProgressText({
        detail: { index: 1, step: "start_zram", total: 3 },
        message: "Starting zram",
      }),
    ).toBe("Starting zram");
    expect(
      swapProgressText({
        detail: {
          device: "/dev/vdb1",
          index: 1,
          step: "drain_device",
          total: 1,
        },
        message: "Moving swapped data back into memory",
      }),
    ).toBe("Moving swapped data back into memory");
    expect(swapProgressText(undefined)).toBeUndefined();
  });
});
