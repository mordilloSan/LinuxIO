import { describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { render, screen } from "@/test/render";
import { formatFileSize } from "@/utils/formaters";

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
      errors: noErrors,
    },
    {
      id: 2,
      path: "",
      sizeBytes: 0,
      usedBytes: 0,
      missing: true,
      errors: { ...noErrors, read: 2, corruption: 1 },
    },
  ],
  scrub: { status: "none", errorCount: 0, bytesScrubbed: 0, percent: 0 },
  degraded: true,
};

const healthy = {
  ...filesystem,
  degraded: false,
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

  it("shows Errors when a device counter is non-zero", () => {
    render(<BtrfsHealthBanner filesystem={filesystem} />);
    expect(screen.getByText("Errors")).toBeInTheDocument();
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
            bytesScrubbed: 1,
            percent: 100,
          },
        }}
      />,
    );
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    expect(screen.getByText(/Last check: .*, 7 errors/)).toBeInTheDocument();
  });

  it("shows scrub progress while running", () => {
    render(
      <BtrfsHealthBanner
        filesystem={{
          ...healthy,
          scrub: {
            status: "running",
            errorCount: 0,
            bytesScrubbed: 1,
            percent: 42,
          },
        }}
      />,
    );
    expect(screen.getByText("Checking for errors · 42%")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stop check" })).toBeEnabled();
  });
});

describe("BtrfsDevicesCard", () => {
  it("lists devices with summed error counts and a Missing chip", () => {
    render(<BtrfsDevicesCard filesystem={filesystem} />);
    expect(screen.getByText("/dev/sda")).toBeInTheDocument();
    expect(screen.getByText("Missing")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText("60 GB used · 40 GB free")).toBeInTheDocument();
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
  it("shows allocation usage and unallocated bytes", () => {
    render(<BtrfsSpaceCard filesystem={filesystem} />);
    expect(screen.getByText("data · single")).toBeInTheDocument();
    expect(
      screen.getByText(formatFileSize(filesystem.unallocatedBytes)),
    ).toBeInTheDocument();
    expect(screen.getByText("uuid-1")).toBeInTheDocument();
  });
});
