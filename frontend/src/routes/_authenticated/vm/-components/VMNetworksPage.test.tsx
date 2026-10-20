import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, within } from "@/test/render";

import VMNetworksPage from "./VMNetworksPage";

const mocks = vi.hoisted(() => ({
  networks: vi.fn(),
  vms: vi.fn(),
}));

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const query = (key: string, data: unknown) => ({
    queryKey: [key],
    queryFn: () => Promise.resolve(data),
  });
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      monitoring: {
        ...actual.linuxio.monitoring,
        get_live: query("test-live", {
          interfaces: {
            eno1: { rx_bytes_per_sec: 15 * 1024, tx_bytes_per_sec: 21 * 1024 },
          },
        }),
      },
      network: {
        ...actual.linuxio.network,
        get_network_info: query("test-nics", [
          { name: "eno1", speed: "10000 Mbps" },
        ]),
      },
      system: {
        ...actual.linuxio.system,
        get_host_info: query("test-host", { hostname: "TNAS" }),
      },
      virt: {
        ...actual.linuxio.virt,
        list: { queryKey: ["test-vms"], queryFn: () => mocks.vms() },
        networks: {
          queryKey: ["test-networks"],
          queryFn: () => mocks.networks(),
        },
      },
    },
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.networks.mockResolvedValue([
    {
      active: true,
      bridge: "br0",
      hasPhysicalUplink: true,
      name: "br0",
      type: "bridge",
      uplink: "eno1",
    },
    {
      active: true,
      bridge: "virbr0",
      name: "default",
      subnet: "192.168.122.1/24",
      type: "libvirt",
    },
  ]);
  mocks.vms.mockResolvedValue([
    {
      name: "ha",
      nics: [
        {
          attachmentType: "bridge",
          ipAddresses: ["192.168.1.103"],
          mac: "52:54:00:56:c9:5a",
          model: "virtio",
          network: "br0",
          receiveBytesPerSecond: 2048,
          sendBytesPerSecond: 0,
        },
      ],
      state: "running",
    },
  ]);
});

describe("VM networks page", () => {
  it("shows a bridge card with the host uplink and its VMs", async () => {
    const user = userEvent.setup();
    render(<VMNetworksPage />);
    const card = await screen.findByRole("region", { name: "br0" });
    expect(within(card).getByText("Normal")).toBeInTheDocument();
    expect(
      within(card).getByText("Type: Bridge (external)"),
    ).toBeInTheDocument();
    // Uplink appears in the key/values and again in the host table.
    expect(within(card).getAllByText("eno1")).toHaveLength(2);
    expect(within(card).getByText("1")).toBeInTheDocument();

    const host = within(card).getByRole("table", { name: "br0 host" });
    expect(within(host).getByText("TNAS")).toBeInTheDocument();
    expect(within(host).getByText("10000 Mbps")).toBeInTheDocument();
    expect(within(host).getByText(/15 kB\/s/)).toBeInTheDocument();
    expect(within(host).getByText(/21 kB\/s/)).toBeInTheDocument();

    await user.click(
      within(card).getByRole("button", { name: "Virtual machines" }),
    );
    const vms = within(card).getByRole("table", {
      name: "br0 virtual machines",
    });
    expect(within(vms).getByText("ha")).toBeInTheDocument();
    expect(within(vms).getByText("52:54:00:56:c9:5a")).toBeInTheDocument();
    expect(within(vms).getByText("192.168.1.103")).toBeInTheDocument();
    expect(within(vms).getByText(/2\.0 kB\/s/)).toBeInTheDocument();
  });

  it("shows the NAT network's subnet and no VM", async () => {
    render(<VMNetworksPage />);
    const card = await screen.findByRole("region", { name: "default" });
    expect(within(card).getByText("Type: NAT (internal)")).toBeInTheDocument();
    expect(within(card).getByText("192.168.122.1/24")).toBeInTheDocument();
    expect(within(card).getByText("Host NAT")).toBeInTheDocument();
    expect(within(card).getByText("0")).toBeInTheDocument();
  });

  it("collapses a card", async () => {
    const user = userEvent.setup();
    render(<VMNetworksPage />);
    const card = await screen.findByRole("region", { name: "br0" });
    await user.click(
      within(card).getByRole("button", { name: "Collapse br0" }),
    );
    expect(
      within(card).getByRole("button", { name: "Expand br0" }),
    ).toHaveAttribute("aria-expanded", "false");
  });
});
