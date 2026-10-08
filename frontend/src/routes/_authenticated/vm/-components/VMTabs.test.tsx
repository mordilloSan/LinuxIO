import { describe, expect, it, vi } from "vitest";

import { render, screen, within } from "@/test/render";

import { VMNetworksTab } from "./VMTabs";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

describe("VM networks tab", () => {
  it("shows the explicit NIC attachment type", () => {
    render(
      <VMNetworksTab
        networks={[]}
        vms={[
          {
            autostart: false,
            diskGB: 1,
            hasGraphics: false,
            memoryMB: 512,
            name: "nat-vm",
            nics: [
              {
                attachmentType: "nat",
                mac: "52:54:00:00:00:01",
                network: "default",
              },
            ],
            ownedDisks: [],
            state: "running",
            vcpus: 1,
          },
          {
            autostart: false,
            diskGB: 1,
            hasGraphics: false,
            memoryMB: 512,
            name: "bridge-vm",
            nics: [
              {
                attachmentType: "bridge",
                mac: "52:54:00:00:00:02",
                network: "br0",
              },
            ],
            ownedDisks: [],
            state: "running",
            vcpus: 1,
          },
        ]}
      />,
    );

    expect(
      screen.getByRole("columnheader", { name: "Attachment" }),
    ).toBeInTheDocument();
    expect(screen.getByText("NAT")).toBeInTheDocument();
    expect(screen.getByText("Bridge")).toBeInTheDocument();
  });

  it("lists host networks with their state and attached VM count", () => {
    render(
      <VMNetworksTab
        networks={[
          {
            active: true,
            hasPhysicalUplink: true,
            name: "br0",
            type: "bridge",
          },
          { active: false, name: "default", type: "libvirt" },
        ]}
        vms={[
          {
            autostart: false,
            diskGB: 1,
            hasGraphics: false,
            memoryMB: 512,
            name: "bridge-vm",
            nics: [{ attachmentType: "bridge", mac: "m1", network: "br0" }],
            ownedDisks: [],
            state: "running",
            vcpus: 1,
          },
        ]}
      />,
    );

    const table = screen.getByRole("table", { name: "Host networks" });
    const rows = within(table).getAllByRole("row");
    expect(within(rows[1]).getByText("br0")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Physical NIC")).toBeInTheDocument();
    expect(within(rows[1]).getByText("1")).toBeInTheDocument();
    expect(within(rows[2]).getByText("default")).toBeInTheDocument();
    expect(within(rows[2]).getByText("inactive")).toBeInTheDocument();
    expect(within(rows[2]).getByText("0")).toBeInTheDocument();
  });
});
