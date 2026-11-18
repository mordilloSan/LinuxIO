import { describe, expect, it } from "vitest";

import { modeResultWarning } from "@/tasks/memoryFeedback";
import {
  foreignZramConfig,
  generatorDevice,
  swapFile,
  swapFileDevice,
  swapModeResult,
  swapStatus,
  zramDevice,
} from "@/test/memoryStatusFixture";

import {
  bannerView,
  canApply,
  confirmView,
  generatorFilesNote,
  initialSelection,
  presetSwappiness,
  rowViews,
  swapFileNote,
} from "./memoryStatusView";

/** A swap file in a folder users other than root can change. */
const openSwapFile = (active: boolean) =>
  swapFile({
    active,
    managed: false,
    path: "/home/ana/swapfile",
    unmanaged_reason: "others_can_write",
  });

const GIB = 1024 ** 3;

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

  it("offers no action when a swap partition blocks zram", () => {
    const view = bannerView(
      swapStatus({
        devices: [],
        mode: "none",
        zram_available: false,
        zram_provider: "",
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
      "unit_held_off",
      "This swap device is switched off for the next start, so LinuxIO will not empty it",
    ],
    [
      "others_can_write",
      "This swap device is in a folder that users other than root can change, so LinuxIO will not empty it",
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
        zram_provider: "",
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

  it("disables zswap and disk when the only swap file is one LinuxIO will not turn on", () => {
    const rows = rowViews(swapStatus({ swap_file: openSwapFile(false) }));
    const reason =
      "LinuxIO will not turn on the swap file: users other than root can change its folder";
    expect(rows[0].disabledReason).toBe(reason);
    expect(rows[2].disabledReason).toBe(reason);
    expect(rows[1].disabledReason).toBeUndefined();
  });

  it("keeps zswap and disk while another swap file is on", () => {
    const rows = rowViews(
      swapStatus({
        devices: [{ ...swapFileDevice(0), path: "/swap2" }],
        mode: "disk",
        swap_file: openSwapFile(false),
      }),
    );
    expect(rows[0].disabledReason).toBeUndefined();
    expect(rows[2].disabledReason).toBeUndefined();
  });

  it("keeps zswap and disk while such a swap file is on", () => {
    const rows = rowViews(
      swapStatus({
        devices: [swapFileDevice(0)],
        mode: "disk",
        swap_file: openSwapFile(true),
      }),
    );
    expect(rows[0].disabledReason).toBeUndefined();
    expect(rows[2].disabledReason).toBeUndefined();
  });

  it("enables zswap and disk for an active swap partition", () => {
    const rows = rowViews(
      swapStatus({
        devices: [
          { ...swapFileDevice(), kind: "partition", path: "/dev/sda2" },
        ],
        mode: "disk",
        zram_available: false,
        zram_provider: "",
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

  it("says on zswap and disk why leaving zram is refused while another zram-generator file defines a device", () => {
    const status = swapStatus({
      swap_file: swapFile({ active: false }),
      zram_generator_devices: [generatorDevice()],
      zram_provider: "zram-generator",
    });
    const reason =
      "/etc/systemd/zram-generator.conf.d/50-big.conf turns compressed memory on at every start. LinuxIO does not change that file. To choose this, remove that file or take the device out of it.";
    const rows = rowViews(status);
    expect(rows[0].disabledReason).toBe(reason);
    expect(rows[2].disabledReason).toBe(reason);
    expect(rows[1].disabledReason).toBeUndefined();
    expect(canApply(status, "disk")).toBe(false);
    // zram-tools does not read those files.
    expect(
      rowViews({ ...status, zram_provider: "zram-tools" })[2].disabledReason,
    ).toBeUndefined();
  });

  it("says how to turn a package's zram-generator file off", () => {
    const rows = rowViews(
      swapStatus({
        swap_file: swapFile({ active: false }),
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

describe("swapFileNote", () => {
  it("says nothing without a swap file or about one LinuxIO manages", () => {
    expect(swapFileNote(swapStatus())).toBeUndefined();
    expect(swapFileNote(swapStatus({ swap_file: swapFile() }))).toBeUndefined();
  });

  it("says a swap file in a folder others can change is left on, and what to do", () => {
    expect(swapFileNote(swapStatus({ swap_file: openSwapFile(true) }))).toBe(
      "LinuxIO does not manage the swap file /home/ana/swapfile: users other than root can change a folder on its path. LinuxIO will not turn it off or empty it, whatever is chosen here. Move it to a folder only root can write.",
    );
  });

  it("says LinuxIO will not turn such a swap file on", () => {
    expect(swapFileNote(swapStatus({ swap_file: openSwapFile(false) }))).toBe(
      "LinuxIO does not manage the swap file /home/ana/swapfile: users other than root can change a folder on its path. LinuxIO will not turn it on, whatever is chosen here. Move it to a folder only root can write.",
    );
  });

  it("says a swap file fstab does not name is left on", () => {
    expect(
      swapFileNote(
        swapStatus({
          swap_file: swapFile({
            managed: false,
            unmanaged_reason: "not_in_fstab",
          }),
        }),
      ),
    ).toBe(
      "LinuxIO does not manage the swap file /swap.img: /etc/fstab does not name it. LinuxIO will not turn it off or empty it, whatever is chosen here.",
    );
  });

  it("gives no cause for a reason it does not know", () => {
    expect(
      swapFileNote(
        swapStatus({
          swap_file: swapFile({
            managed: false,
            unmanaged_reason: "something_new" as never,
          }),
        }),
      ),
    ).toBe(
      "LinuxIO does not manage the swap file /swap.img. LinuxIO will not turn it off or empty it, whatever is chosen here.",
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

describe("confirmView", () => {
  const SWAP_FILE_STEPS = [
    "Compressed memory is turned on.",
    "Data parked in the swap file is brought back to memory.",
    "The swap file is turned off and stays off after a restart.",
    "The file itself is not deleted.",
  ];
  const replaced = (path: string) =>
    `${path} holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as ${path}.before-linuxio.`;

  it("asks only for zram while the swap file is active", () => {
    const status = swapStatus({ mode: "disk", swap_file: swapFile() });
    expect(confirmView(status, "zram")).toEqual({
      action: "Turn on compressed memory",
      items: SWAP_FILE_STEPS,
      title: "Switch to compressed memory?",
    });
    expect(confirmView(status, "zswap")).toBeUndefined();
    expect(
      confirmView(
        swapStatus({ swap_file: swapFile({ active: false }) }),
        "zram",
      ),
    ).toBeUndefined();
    expect(confirmView(swapStatus(), "zram")).toBeUndefined();
  });

  it("does not ask for a swap file fstab does not name: LinuxIO will not stop it", () => {
    const status = swapStatus({
      mode: "disk",
      swap_file: swapFile({ managed: false }),
    });
    expect(confirmView(status, "zram")).toBeUndefined();
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
    const status = swapStatus({
      foreign_zram_config: foreignZramConfig(),
      mode: "disk",
      swap_file: swapFile(),
    });
    expect(confirmView(status, "zram")?.items).toEqual([
      ...SWAP_FILE_STEPS,
      replaced("/etc/default/zramswap"),
    ]);
  });

  it("asks before leaving zram only when zram-generator's file is replaced then", () => {
    const generator = swapStatus({
      foreign_zram_config: foreignZramConfig(
        "/etc/systemd/zram-generator.conf",
      ),
      swap_file: swapFile({ active: false }),
      zram_provider: "zram-generator",
    });
    expect(confirmView(generator, "disk")).toEqual({
      action: "Apply",
      items: [replaced("/etc/systemd/zram-generator.conf")],
      replace: "/etc/systemd/zram-generator.conf",
      title: "Switch to disk only?",
    });
    expect(confirmView(generator, "zswap")?.title).toBe(
      "Switch to compressed memory with the disk as backup?",
    );
    // zram-tools only disables its service on the way out.
    const tools = swapStatus({
      foreign_zram_config: foreignZramConfig(),
      swap_file: swapFile({ active: false }),
    });
    expect(confirmView(tools, "disk")).toBeUndefined();
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
      "Not enough free memory to bring back the data in the swap file, so it stays on. Apply again when more memory is free.",
    ],
    [
      "swap_file",
      "not_identified",
      "An active swap file could not be matched to the one in /etc/fstab, so LinuxIO left it as it is.",
    ],
    [
      "swap_file",
      "not_managed",
      "A swap file that LinuxIO does not manage is still on. LinuxIO manages only one swap file in /etc/fstab.",
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
    expect(
      modeResultWarning(
        swapModeResult(swapStatus({ mode: "mixed" }), [{ reason, what }]),
        "zram",
      ),
    ).toBe(text);
  });

  it("gives every reason when more than one thing was left", () => {
    expect(
      modeResultWarning(
        swapModeResult(swapStatus({ mode: "mixed" }), [
          { reason: "not_managed", what: "zram_size" },
          { reason: "zram_not_swapping", what: "swap_file" },
        ]),
        "zram",
      ),
    ).toBe(
      "Compressed memory keeps its current size: it is not run by a zram tool that LinuxIO manages. Compressed memory is not running, so the swap file stays on.",
    );
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
