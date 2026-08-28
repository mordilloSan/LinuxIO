import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import { VMNetworksTab } from "./VMTabs";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

describe("VM networks tab", () => {
  it("shows the explicit NIC attachment type", () => {
    render(
      <VMNetworksTab
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
});
