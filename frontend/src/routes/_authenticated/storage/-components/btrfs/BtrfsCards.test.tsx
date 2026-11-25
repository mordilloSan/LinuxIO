import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { render, screen } from "@/test/render";
import { formatFileSize } from "@/utils/formatters";

import BtrfsDevicesCard from "./BtrfsDevicesCard";
import BtrfsHealthBanner from "./BtrfsHealthBanner";
import BtrfsSpaceCard from "./BtrfsSpaceCard";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const noErrors = { write: 0, read: 0, flush: 0, corruption: 0, generation: 0 };

const filesystem: BtrfsFilesystem = {
  uuid: "uuid-1",
  label: "data",
  mountpoints: [],
  totalBytes: 100 * 1024 ** 3,
  unallocatedBytes: 40 * 1024 ** 3,
  usedBytes: 30 * 1024 ** 3,
  storedBytes: 25 * 1024 ** 3,
  freeBytes: 65 * 1024 ** 3,
  allocation: [
    {
      kind: "data",
      profile: "single",
      totalBytes: 50 * 1024 ** 3,
      usedBytes: 25 * 1024 ** 3,
    },
  ],
  devices: [
    {
      id: 1,
      path: "/dev/sda",
      sizeBytes: 100 * 1024 ** 3,
      usedBytes: 60 * 1024 ** 3,
      missing: false,
      physicalBytes: 0,
      growable: false,
      errors: noErrors,
    },
    {
      id: 2,
      path: "",
      sizeBytes: 0,
      usedBytes: 0,
      missing: true,
      physicalBytes: 0,
      growable: false,
      errors: { ...noErrors, read: 2, corruption: 1 },
    },
  ],
  scrub: {
    status: "none",
    errorCount: 0,
    uncorrectableErrors: 0,
    correctedErrors: 0,
    bytesScrubbed: 0,
    percent: 0,
  },
  degraded: true,
  lowMetadataSpace: false,
  autoRebuild: true,
};

const healthy = {
  ...filesystem,
  degraded: false,
  lowMetadataSpace: false,
  autoRebuild: true,
  devices: [filesystem.devices[0]],
};

describe("BtrfsHealthBanner", () => {
  it("shows Degraded when degraded without device errors", () => {
    render(<BtrfsHealthBanner filesystem={{ ...filesystem, devices: [] }} />);
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    expect(screen.getByText("Never checked for errors")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Check for errors" }),
    ).toBeEnabled();
  });

  it("names the filesystem and its mount point", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...healthy,
          mountpoints: [{ mountpoint: "/mnt/data" } as never],
        }}
      />,
    );
    expect(
      screen.getByText("data at /mnt/data is working normally."),
    ).toBeInTheDocument();
  });

  it("shows Errors when a present device's counter is non-zero", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...filesystem,
          devices: [
            { ...filesystem.devices[0], errors: { ...noErrors, write: 3 } },
            filesystem.devices[1],
          ],
        }}
      />,
    );
    expect(screen.getByText("Errors")).toBeInTheDocument();
  });

  // Since 7.1 a pulled disk is marked missing with the writes it failed.
  it("shows Degraded when only the missing disk counts errors", () => {
    render(<BtrfsHealthBanner filesystem={filesystem} />);
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    expect(screen.queryByText("Errors")).not.toBeInTheDocument();
  });

  it("shows Healthy and the finished scrub error count", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...healthy,
          scrub: {
            status: "finished",
            finishedAt: "2026-10-01T10:00:00Z",
            errorCount: 7,
            uncorrectableErrors: 0,
            correctedErrors: 0,
            bytesScrubbed: 1,
            percent: 100,
          },
        }}
      />,
    );
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    expect(screen.getByText(/Last check: .*, 7 errors/)).toBeInTheDocument();
  });

  it("offers to replace the missing disk when degraded", async () => {
    const user = userEvent.setup();
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...filesystem,
          devices: filesystem.devices.map((d) => ({ ...d, errors: noErrors })),
        }}
      />,
    );
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Replace missing disk" }),
    );
    expect(
      screen.getByRole("dialog", { name: "Replace missing disk" }),
    ).toBeInTheDocument();
  });

  it("offers reconnecting the missing disk only with the automatic rebuild on", () => {
    const degraded = {
      ...filesystem,
      devices: filesystem.devices.map((d) => ({ ...d, errors: noErrors })),
    };
    const { unmount } = render(<BtrfsHealthBanner filesystem={degraded} />);
    expect(
      screen.getByText(/Reconnect it, and LinuxIO rebuilds the pool onto it/),
    ).toBeInTheDocument();
    unmount();

    render(
      <BtrfsHealthBanner filesystem={{ ...degraded, autoRebuild: false }} />,
    );
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    expect(screen.queryByText(/Reconnect it/)).not.toBeInTheDocument();
  });

  it("says a disk dropped out, ahead of the errors its failed writes count", async () => {
    const user = userEvent.setup();
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...filesystem,
          autoRebuild: false,
          devices: [
            filesystem.devices[0],
            {
              ...filesystem.devices[1],
              path: "/dev/sdb",
              droppedOut: true,
              errors: { ...noErrors, write: 40, flush: 3 },
            },
          ],
        }}
      />,
    );
    expect(screen.getByText("Disk dropped out")).toBeInTheDocument();
    expect(
      screen.getByText(
        /\/dev\/sdb dropped out of data while it was mounted.*takes the disk back only when it is mounted again/,
      ),
    ).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Replace missing disk" }),
    );
    expect(
      screen.getByRole("dialog", { name: "Replace missing disk" }),
    ).toBeInTheDocument();
  });

  it("tells a rebuild that runs, failed or was refused ahead of the missing disk", () => {
    const dropped = {
      ...filesystem,
      devices: [
        filesystem.devices[0],
        { ...filesystem.devices[1], path: "/dev/sdb", droppedOut: true },
      ],
    };
    const { rerender } = render(
      <BtrfsHealthBanner
        filesystem={{
          ...dropped,
          rebuild: {
            state: "running",
            deviceId: 2,
            device: "/dev/sdf",
          },
        }}
      />,
    );
    expect(screen.getByText("Rebuilding")).toBeInTheDocument();
    expect(
      screen.getByText(
        "LinuxIO is rebuilding device 2 of data onto /dev/sdf, the disk that came back, from the pool's other disks. The pool stays mounted; once the copy is checked, LinuxIO resyncs the pool.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Replace missing disk" }),
    ).not.toBeInTheDocument();

    rerender(
      <BtrfsHealthBanner
        filesystem={{
          ...dropped,
          rebuild: {
            state: "running",
            deviceId: 2,
            device: "/dev/sdv",
            spare: true,
          },
        }}
      />,
    );
    expect(
      screen.getByText(/onto \/dev\/sdv, the hot spare,/),
    ).toBeInTheDocument();

    rerender(
      <BtrfsHealthBanner
        filesystem={{
          ...dropped,
          rebuild: {
            state: "failed",
            deviceId: 2,
            device: "/dev/sdf",
            reason: "the pool went read-only",
          },
        }}
      />,
    );
    expect(screen.getByText("Rebuild failed")).toBeInTheDocument();
    expect(
      screen.getByText(
        "The automatic rebuild of device 2 onto /dev/sdf failed: the pool went read-only. LinuxIO does not try that disk again by itself; replace the disk.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Replace missing disk" }),
    ).toBeInTheDocument();

    rerender(
      <BtrfsHealthBanner
        filesystem={{
          ...dropped,
          rebuild: {
            state: "refused",
            reason: "/dev/sdf is not the disk that was device 2",
          },
        }}
      />,
    );
    expect(screen.getByText("Not rebuilt automatically")).toBeInTheDocument();
    expect(
      screen.getByText(
        "/dev/sdf is not the disk that was device 2. Replace the missing disk.",
      ),
    ).toBeInTheDocument();

    rerender(
      <BtrfsHealthBanner
        filesystem={{
          ...dropped,
          rebuild: { state: "waiting", deviceId: 2, device: "/dev/sdf" },
        }}
      />,
    );
    expect(screen.getByText("Disk back")).toBeInTheDocument();
    expect(
      screen.getByText(
        "/dev/sdf is back as device 2 of data. LinuxIO erases it and rebuilds the pool onto it once the next health check, within 10 minutes, sees it too.",
      ),
    ).toBeInTheDocument();
  });

  it("gives a refusal's own advice in place of the replace it warns against", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...filesystem,
          degraded: true,
          rebuild: {
            state: "refused",
            reason:
              "device 2 holds data single chunks, which keep one copy, so their only copy is on its disk",
            advice:
              "Reconnect that disk and mount the pool again so btrfs takes it back, or copy the data off the pool.",
          },
        }}
      />,
    );
    expect(
      screen.getByText(
        "Device 2 holds data single chunks, which keep one copy, so their only copy is on its disk. Reconnect that disk and mount the pool again so btrfs takes it back, or copy the data off the pool.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/Replace the missing disk/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Replace missing disk" }),
    ).not.toBeInTheDocument();
  });

  it("says a rebuild on record waits, not that it copies", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...filesystem,
          degraded: true,
          rebuild: {
            state: "waiting",
            deviceId: 2,
            device: "/dev/sdf",
            reason:
              "the rebuild of device 2 was cut and starts again once /dev/sdf, its disk, is connected",
          },
        }}
      />,
    );
    expect(screen.getByText("Rebuild waits")).toBeInTheDocument();
    expect(
      screen.getByText(
        "The rebuild of device 2 was cut and starts again once /dev/sdf, its disk, is connected. Nothing is copied meanwhile.",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("Rebuilding")).not.toBeInTheDocument();
  });

  it("says LinuxIO rebuilds onto a disk that dropped out once it is back", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...filesystem,
          autoRebuild: true,
          devices: [
            filesystem.devices[0],
            { ...filesystem.devices[1], path: "/dev/sdb", droppedOut: true },
          ],
        }}
      />,
    );
    expect(
      screen.getByText(
        /\/dev\/sdb dropped out of data while it was mounted, and the pool runs on its other disks\. Reconnect the disk: LinuxIO rebuilds the pool onto it once two health checks in a row have seen it back\./,
      ),
    ).toBeInTheDocument();
  });

  it("reports uncorrectable scrub errors ahead of a healthy state", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...healthy,
          scrub: {
            status: "finished",
            finishedAt: "2026-10-01T10:00:00Z",
            errorCount: 3,
            uncorrectableErrors: 2,
            correctedErrors: 1,
            bytesScrubbed: 1,
            percent: 100,
          },
        }}
      />,
    );
    expect(screen.getByText("Data errors")).toBeInTheDocument();
    expect(
      screen.getByText(/3 errors \(2 uncorrectable\)/),
    ).toBeInTheDocument();
  });

  it("offers to reclaim space when metadata is nearly full", () => {
    render(
      <BtrfsHealthBanner filesystem={{ ...healthy, lowMetadataSpace: true }} />,
    );
    expect(screen.getByText("Low space")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reclaim space" })).toBeEnabled();
  });

  it("shows scrub progress while running", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...healthy,
          scrub: {
            status: "running",
            errorCount: 0,
            uncorrectableErrors: 0,
            correctedErrors: 0,
            bytesScrubbed: 1,
            percent: 18.986434994665448,
          },
        }}
      />,
    );
    expect(screen.getByText("Checking for errors · 19.0%")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stop check" })).toBeEnabled();
  });
});

describe("BtrfsDevicesCard", () => {
  it("lists devices with summed error counts and a Missing chip", () => {
    render(<BtrfsDevicesCard filesystem={filesystem} />);
    expect(screen.getByText("/dev/sda")).toBeInTheDocument();
    expect(screen.getByText("Missing")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText("60 GB allocated")).toBeInTheDocument();
    expect(screen.queryByText(/free/)).not.toBeInTheDocument();
    expect(screen.getByText("2 disks")).toBeInTheDocument();
  });

  it("counts one disk without a plural", () => {
    render(
      <BtrfsDevicesCard
        filesystem={{ ...filesystem, devices: [filesystem.devices[0]] }}
      />,
    );
    expect(screen.getByText("1 disk")).toBeInTheDocument();
  });

  it("marks a disk that dropped out of the mounted pool as such", () => {
    render(
      <BtrfsDevicesCard
        filesystem={{
          ...filesystem,
          devices: [
            filesystem.devices[0],
            { ...filesystem.devices[1], path: "/dev/sdb", droppedOut: true },
          ],
        }}
      />,
    );
    expect(screen.getByText("/dev/sdb")).toBeInTheDocument();
    expect(screen.getByText("Dropped out")).toBeInTheDocument();
    expect(screen.queryByText("Missing")).not.toBeInTheDocument();
  });

  it("keeps the last present device", () => {
    render(<BtrfsDevicesCard filesystem={filesystem} />);
    // /dev/sda is the only device that is not missing.
    for (const button of screen.getAllByRole("button", { name: "Remove" })) {
      expect(button).toBeDisabled();
    }
  });
});

describe("BtrfsSpaceCard", () => {
  it("shows what is stored and about how much is free", () => {
    render(<BtrfsSpaceCard filesystem={filesystem} />);
    expect(screen.getByText("25 GB")).toBeInTheDocument();
    expect(screen.getByText("About 65 GB free of 90 GB")).toBeInTheDocument();
    expect(
      screen.getByRole("progressbar", { name: "Space used" }),
    ).toBeInTheDocument();
    // A figure, not a heading.
    expect(
      screen.queryByRole("heading", { name: "25 GB" }),
    ).not.toBeInTheDocument();
  });

  it("keeps how the disk is laid out behind Details", async () => {
    const user = userEvent.setup();
    render(<BtrfsSpaceCard filesystem={filesystem} />);
    expect(screen.queryByText("Data · single")).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Details: how the disk is laid out" }),
    );

    expect(screen.getByText("Data · single")).toBeInTheDocument();
    expect(
      screen.getByText("25 GB used of 50 GB set aside"),
    ).toBeInTheDocument();
    expect(screen.getByText("Not set aside yet")).toBeInTheDocument();
    expect(
      screen.getByText(formatFileSize(filesystem.unallocatedBytes)),
    ).toBeInTheDocument();
    expect(screen.getByText("uuid-1")).toBeInTheDocument();
  });

  it("shows no figures when the space could not be read", () => {
    render(
      <BtrfsSpaceCard
        filesystem={{
          ...filesystem,
          allocation: [],
          storedBytes: 0,
          freeBytes: 0,
        }}
      />,
    );
    expect(screen.getByText("Space could not be read")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Details/ }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/free of/)).not.toBeInTheDocument();
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });
});
