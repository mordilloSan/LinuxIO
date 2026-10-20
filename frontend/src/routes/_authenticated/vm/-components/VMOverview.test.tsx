import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { MonitoringLive, VMPreflight, VirtualMachine } from "@/api";
import { render, screen, within } from "@/test/render";

import VMOverview from "./VMOverview";

const preflight = {
  defaultNetworkActive: true,
  defaultNetworkExists: true,
  defaultPoolActive: true,
  defaultPoolExists: true,
  firmware: { biosAvailable: true, uefiAvailable: true },
  isoReadable: true,
  kvmPresent: true,
  libvirtReachable: true,
  managedPaths: { cloudImages: "", isos: "", root: "" },
  qemuPresent: true,
} satisfies VMPreflight;

const live = {
  cpu: { percent: 2.5 },
  interfaces: {
    eno1: { rx_bytes_per_sec: 15 * 1024, tx_bytes_per_sec: 10 * 1024 },
  },
  memory: { total_bytes: 1000, used_bytes: 181 },
} as unknown as MonitoringLive;

const vms: VirtualMachine[] = [
  {
    autostart: false,
    diskGB: 32,
    hasGraphics: true,
    memoryMB: 4096,
    metrics: { cpuPercent: 12.5, memoryUsedMB: 1024 },
    name: "ha",
    ownedDisks: [],
    state: "running",
    vcpus: 2,
  },
  {
    autostart: false,
    diskGB: 8,
    hasGraphics: false,
    memoryMB: 1024,
    name: "off",
    ownedDisks: [],
    state: "shutoff",
    vcpus: 1,
  },
];

const pools = [
  {
    active: true,
    allocationBytes: 345 * 1024 ** 3,
    availableBytes: 651 * 1024 ** 3,
    capacityBytes: 996 * 1024 ** 3,
    name: "default",
    path: "/var/lib/libvirt/images",
    type: "dir",
    volumes: [],
  },
];

const renderOverview = (overrides: Partial<VMPreflight> = {}) =>
  render(
    <VMOverview
      hostname="TNAS"
      interfaces={[
        { name: "eno1", operstate: "up" } as never,
        { name: "veth1", operstate: "up" } as never,
      ]}
      live={live}
      pools={pools}
      preflight={{ ...preflight, ...overrides }}
      vms={vms}
    />,
  );

describe("VM overview", () => {
  it("summarises health, counts and the host, VM and storage rows", () => {
    renderOverview();
    expect(screen.getByRole("heading", { name: "Normal" })).toBeInTheDocument();
    expect(screen.getByText("Running of 2")).toBeInTheDocument();

    const host = screen.getByRole("listitem", { name: "TNAS" });
    expect(within(host).getByText("2.50%")).toBeInTheDocument();
    expect(within(host).getByText("18.10%")).toBeInTheDocument();
    expect(within(host).getByText("eno1")).toBeInTheDocument();
    expect(within(host).getByText(/15 kB\/s/)).toBeInTheDocument();

    const ha = screen.getByRole("listitem", { name: "ha" });
    expect(within(ha).getByText("12.50%")).toBeInTheDocument();
    expect(within(ha).getByText("25.00%")).toBeInTheDocument();
    expect(
      within(screen.getByRole("listitem", { name: "off" })).getAllByText("-"),
    ).toHaveLength(4);

    const pool = screen.getByRole("listitem", { name: "TNAS default" });
    expect(within(pool).getByText("345 GB / 996 GB")).toBeInTheDocument();
    expect(within(pool).getByRole("progressbar")).toHaveAttribute(
      "aria-valuenow",
      "35",
    );
  });

  it("filters the list by kind", async () => {
    const user = userEvent.setup();
    renderOverview();
    await user.click(screen.getByRole("button", { name: "Storage" }));
    expect(
      screen.queryByRole("listitem", { name: "TNAS" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("listitem", { name: "ha" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("listitem", { name: "TNAS default" }),
    ).toBeInTheDocument();
  });

  it("turns the banner red when preflight reports errors", () => {
    renderOverview({ errors: ["KVM is not available"] });
    expect(screen.getByRole("heading", { name: "Error" })).toBeInTheDocument();
    expect(screen.getByText("KVM is not available")).toBeInTheDocument();
  });
});
