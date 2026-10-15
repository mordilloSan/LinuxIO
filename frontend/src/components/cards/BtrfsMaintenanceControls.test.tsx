import { describe, expect, it, vi } from "vitest";

import { render, renderWithTanStackRouter, screen } from "@/test/render";

import BtrfsMaintenanceControls from "./BtrfsMaintenanceControls";

const schedule = {
  tasks: [
    {
      task: "scrub",
      period: "monthly",
      periods: ["none", "weekly", "monthly"],
      mountpoints: ["/", "/home"],
    },
    {
      task: "defrag",
      period: "none",
      periods: ["none", "daily", "weekly", "monthly"],
      mountpoints: [],
    },
  ],
};

describe("BtrfsMaintenanceControls", () => {
  it("explains each task and warns when a period is shared", async () => {
    render(
      <BtrfsMaintenanceControls
        btrfsMaintenance={schedule}
        btrfsMaintenanceReason={null}
        isUpdatingMaintenance={false}
        mountpoint="/home"
        onSetMaintenance={vi.fn()}
      />,
    );

    expect(
      await screen.findByText(/checks it against its checksum/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Shared with \/, so a new period applies there too/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Breaks sharing with snapshots/)).toBeVisible();
    expect(
      screen.getByRole("combobox", { name: "Scrub period" }),
    ).toHaveTextContent("Monthly");
    expect(
      screen.getByRole("combobox", { name: "Defrag period" }),
    ).toHaveTextContent("Off");
  });

  it("links to Capabilities when btrfsmaintenance is missing", async () => {
    renderWithTanStackRouter(
      <BtrfsMaintenanceControls
        btrfsMaintenance={null}
        btrfsMaintenanceReason="btrfsmaintenance is not installed."
        isUpdatingMaintenance={false}
        mountpoint="/"
        onSetMaintenance={vi.fn()}
      />,
    );

    expect(
      await screen.findByRole("link", { name: /install btrfsmaintenance/ }),
    ).toBeInTheDocument();
  });
});
