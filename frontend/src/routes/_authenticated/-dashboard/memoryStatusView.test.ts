import { describe, expect, it } from "vitest";

import {
  swapFile,
  swapFileDevice,
  swapStatus,
  zramDevice,
} from "@/test/memoryStatusFixture";

import {
  bannerView,
  canApply,
  initialSelection,
  mixedResultMessage,
  needsSwapFileConfirm,
  presetSwappiness,
  rowViews,
} from "./memoryStatusView";

const GIB = 1024 ** 3;

describe("bannerView", () => {
  it("warns that programs will be closed when nothing absorbs overflow", () => {
    expect(bannerView(swapStatus({ devices: [], mode: "none" }))).toEqual({
      action: { kind: "enable_zram" },
      severity: "warning",
      text: "If memory runs out, Linux will close programs.",
    });
  });

  it("links to Capabilities instead when zram-tools is missing", () => {
    const view = bannerView(
      swapStatus({
        devices: [],
        mode: "none",
        zram_available: false,
        zram_unavailable_reason: "package_missing",
      }),
    );
    expect(view.action).toEqual({ kind: "capabilities" });
  });

  it("offers no action when a swap partition blocks zram", () => {
    const view = bannerView(
      swapStatus({
        devices: [],
        mode: "none",
        zram_available: false,
        zram_unavailable_reason: "swap_partition_active",
      }),
    );
    expect(view.action).toEqual({ kind: "none" });
    expect(view.severity).toBe("warning");
  });

  it("asks to finish a switch that left both swap kinds active", () => {
    expect(
      bannerView(
        swapStatus({
          devices: [zramDevice, swapFileDevice(GIB)],
          mode: "mixed",
        }),
      ),
    ).toEqual({
      action: { kind: "none" },
      severity: "warning",
      text: "Compressed memory and the swap file are both active. Choose one below and apply to finish.",
    });
  });

  it("offers to bring parked data back when there is room", () => {
    expect(
      bannerView(
        swapStatus({
          can_drain: true,
          devices: [swapFileDevice(1.5 * GIB)],
          drain_blocked_reason: "",
          mode: "disk",
        }),
      ),
    ).toEqual({
      action: { blockedReason: undefined, canDrain: true, kind: "drain" },
      severity: "info",
      text: "1.5 GB of memory is parked on the system drive. Programs that need it are slower until it comes back.",
    });
  });

  it("does not count zram as parked data", () => {
    const view = bannerView(
      swapStatus({
        devices: [{ ...zramDevice, used: 3 * GIB }, swapFileDevice(0)],
        mode: "mixed",
      }),
    );
    expect(view.text).toMatch(/both active/);
    expect(
      bannerView(swapStatus({ devices: [{ ...zramDevice, used: 3 * GIB }] }))
        .text,
    ).toBe("Memory is fine. Nothing is parked.");
  });

  it.each([
    ["not_enough_memory", "Not enough free memory right now"],
    [
      "unmanaged_device",
      "This swap device is not in /etc/fstab, so LinuxIO cannot restart it",
    ],
    [
      "unit_masked",
      "This swap device is switched off for the next start, so LinuxIO will not empty it",
    ],
    ["something_new", undefined],
  ] as const)("explains a blocked drain for %s", (code, reason) => {
    const view = bannerView(
      swapStatus({
        can_drain: false,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: code as never,
        mode: "disk",
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

  it("warns that a btrfs swap file blocks snapshots", () => {
    expect(
      bannerView(
        swapStatus({
          devices: [swapFileDevice(0, "btrfs")],
          mode: "disk",
          swap_file: swapFile({ fs_type: "btrfs" }),
        }),
      ),
    ).toEqual({
      action: { kind: "switch_zram" },
      severity: "warning",
      text: "The swap file blocks snapshots of the system drive.",
    });
  });

  it("hides the switch when zram cannot be chosen", () => {
    const view = bannerView(
      swapStatus({
        devices: [swapFileDevice(0, "btrfs")],
        mode: "disk",
        swap_file: swapFile({ fs_type: "btrfs" }),
        zram_available: false,
        zram_unavailable_reason: "package_missing",
      }),
    );
    expect(view.action).toEqual({ kind: "none" });
  });

  it("offers no switch for a btrfs swap file fstab does not name", () => {
    expect(
      bannerView(
        swapStatus({
          devices: [swapFileDevice(0, "btrfs")],
          mode: "disk",
          swap_file: swapFile({ fs_type: "btrfs", managed: false }),
        }),
      ),
    ).toEqual({
      action: { kind: "none" },
      severity: "warning",
      text: "The swap file blocks snapshots of the system drive.",
    });
  });

  it("ignores an inactive btrfs swap file", () => {
    expect(
      bannerView(
        swapStatus({
          swap_file: swapFile({ active: false, fs_type: "btrfs" }),
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
  const diskHost = swapStatus({
    devices: [swapFileDevice()],
    mode: "disk",
    recommended_mode: "zswap",
    swap_file: swapFile(),
  });

  it("lists zswap, zram and disk in that order with chips", () => {
    const rows = rowViews(diskHost);
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

  it("disables zswap and disk without a swap file or partition", () => {
    const rows = rowViews(swapStatus());
    expect(rows[0].disabledReason).toBe(
      "Needs a swap file on the system drive",
    );
    expect(rows[2].disabledReason).toBe(
      "Needs a swap file on the system drive",
    );
    expect(rows[1].disabledReason).toBeUndefined();
  });

  it("enables zswap and disk for an active swap partition", () => {
    const rows = rowViews(
      swapStatus({
        devices: [
          { ...swapFileDevice(), kind: "partition", path: "/dev/sda2" },
        ],
        mode: "disk",
        zram_available: false,
        zram_unavailable_reason: "swap_partition_active",
      }),
    );
    expect(rows[0].disabledReason).toBeUndefined();
    expect(rows[2].disabledReason).toBeUndefined();
    expect(rows[1].disabledReason).toBe(
      "A swap partition is active. LinuxIO only manages swap files.",
    );
  });

  it("disables zswap on a kernel without it", () => {
    const rows = rowViews(
      swapStatus({
        swap_file: swapFile(),
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

  it("replaces the zram radio with a Capabilities link when the package is missing", () => {
    const zram = rowViews(
      swapStatus({
        zram_available: false,
        zram_unavailable_reason: "package_missing",
      }),
    )[1];
    expect(zram.capabilitiesLink).toBe(true);
    expect(zram.disabledReason).toBeUndefined();
  });

  it("carries the cautions", () => {
    const rows = rowViews(diskHost);
    expect(rows[0].caution).toBeUndefined();
    expect(rows[1].caution).toBe(
      "No backup. If it fills up, Linux closes the hungriest program, and a VM would stop like a power cut.",
    );
    expect(rows[2].caution).toBe(
      "Blocks snapshots of the system drive while in use.",
    );
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
    expect(canApply(swapStatus({ swap_file: swapFile() }), "disk")).toBe(true);
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
    expect(canApply(swapStatus(), "disk")).toBe(false);
  });
});

describe("needsSwapFileConfirm", () => {
  it("asks only for zram while the swap file is active", () => {
    const status = swapStatus({ mode: "disk", swap_file: swapFile() });
    expect(needsSwapFileConfirm(status, "zram")).toBe(true);
    expect(needsSwapFileConfirm(status, "zswap")).toBe(false);
    expect(
      needsSwapFileConfirm(
        swapStatus({ swap_file: swapFile({ active: false }) }),
        "zram",
      ),
    ).toBe(false);
    expect(needsSwapFileConfirm(swapStatus(), "zram")).toBe(false);
  });

  it("does not ask for a swap file fstab does not name: LinuxIO will not stop it", () => {
    const status = swapStatus({
      mode: "disk",
      swap_file: swapFile({ managed: false }),
    });
    expect(needsSwapFileConfirm(status, "zram")).toBe(false);
  });
});

describe("presetSwappiness", () => {
  it("reads the preset of a mode", () => {
    expect(presetSwappiness(swapStatus(), "zswap")).toBe(60);
    expect(presetSwappiness(swapStatus(), "zram")).toBe(180);
  });
});

describe("mixedResultMessage", () => {
  it("says there is not enough room when LinuxIO could have stopped the file", () => {
    const message =
      "Not enough free memory to finish the switch. Both are active for now; apply again later.";
    expect(
      mixedResultMessage(swapStatus({ mode: "mixed", swap_file: swapFile() })),
    ).toBe(message);
    expect(mixedResultMessage(swapStatus({ mode: "mixed" }))).toBe(message);
    expect(
      mixedResultMessage(
        swapStatus({ mode: "mixed", swap_file: swapFile({ active: false }) }),
      ),
    ).toBe(message);
  });

  it("says LinuxIO left an active swap file that fstab does not name", () => {
    expect(
      mixedResultMessage(
        swapStatus({
          mode: "mixed",
          swap_file: swapFile({ managed: false }),
        }),
      ),
    ).toBe("The swap file is not in /etc/fstab, so LinuxIO left it on.");
  });
});
