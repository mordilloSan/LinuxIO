import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NetworkInterface } from "@/api";
import NetworkInterfaceEditor from "@/components/network/NetworkInterfaceEditor";
import { testNetworkInterface } from "@/test/networkInterface";
import { render, screen } from "@/test/render";

const mocks = vi.hoisted(() => ({
  disableConnection: vi.fn(),
  enableConnection: vi.fn(),
  setIPv4: vi.fn(),
  setIPv4Manual: vi.fn(),
  useCallMutation: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();

  return {
    ...actual,
    useCallMutation: mocks.useCallMutation,
    linuxio: {
      ...actual.linuxio,
      network: {
        ...actual.linuxio.network,
      },
    },
  };
});

mocks.useCallMutation.mockImplementation((endpoint: { route: string }) => {
  const mutate = endpoint.route.endsWith("disable_connection")
    ? mocks.disableConnection
    : endpoint.route.endsWith("enable_connection")
      ? mocks.enableConnection
      : endpoint.route.endsWith("set_ipv4_manual")
        ? mocks.setIPv4Manual
        : mocks.setIPv4;
  return { isPending: false, mutate };
});

const manualInterface = (
  overrides: Partial<NetworkInterface> = {},
): NetworkInterface =>
  testNetworkInterface({
    dns: ["1.1.1.1", "8.8.8.8"],
    gateway: "192.168.1.1",
    ipv4: ["192.168.1.25/24"],
    ipv4_method: "manual",
    ...overrides,
  });

const addressInput = () => screen.getByPlaceholderText("192.168.1.10/24");
const gatewayInput = () => screen.getByPlaceholderText("192.168.1.1");
const dnsInput = () => screen.getByPlaceholderText("8.8.8.8, 8.8.4.4");

describe("NetworkInterfaceEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("opens a manual interface in manual mode with live defaults", () => {
    render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={vi.fn()}
      />,
    );

    expect(addressInput()).toHaveValue("192.168.1.25/24");
    expect(gatewayInput()).toHaveValue("192.168.1.1");
    expect(dnsInput()).toHaveValue("1.1.1.1, 8.8.8.8");
  });

  it("reads back DHCP-assigned addresses instead of offering the form", () => {
    render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({ ipv4_method: "auto" })}
        onClose={vi.fn()}
      />,
    );

    expect(
      screen.getByText(
        "The interface obtains its address, gateway, and DNS from DHCP.",
      ),
    ).toBeVisible();
    expect(screen.getByText("192.168.1.25/24")).toBeVisible();
    expect(screen.getByText("192.168.1.1")).toBeVisible();
    expect(screen.queryByLabelText("IPv4 Address (CIDR)")).toBeNull();
  });

  it("keeps the interface, its mode switch and its form in one card", () => {
    const { container } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={vi.fn()}
      />,
    );

    expect(container.querySelectorAll(".app-grid-item")).toHaveLength(1);
    expect(screen.getByText("eth0")).toBeVisible();
    expect(screen.getByRole("button", { name: "Manual" })).toBeVisible();
    expect(addressInput()).toBeVisible();
  });

  it("accepts polling defaults until editing starts, then preserves the draft", async () => {
    const onClose = vi.fn();
    const { rerender, user } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={onClose}
      />,
    );

    rerender(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({ ipv4: ["192.168.1.30/24"] })}
        onClose={onClose}
      />,
    );
    expect(addressInput()).toHaveValue("192.168.1.30/24");

    await user.clear(addressInput());
    await user.type(addressInput(), "10.0.0.20/24");
    rerender(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({ ipv4: ["192.168.1.40/24"] })}
        onClose={onClose}
      />,
    );

    expect(addressInput()).toHaveValue("10.0.0.20/24");
  });

  it("starts a fresh draft when the editor is closed and reopened", async () => {
    const onClose = vi.fn();
    const { rerender, user } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={onClose}
      />,
    );

    await user.clear(addressInput());
    await user.type(addressInput(), "10.0.0.20/24");

    rerender(
      <NetworkInterfaceEditor
        expanded={false}
        iface={manualInterface({ ipv4: ["192.168.1.50/24"] })}
        onClose={onClose}
      />,
    );
    rerender(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({ ipv4: ["192.168.1.50/24"] })}
        onClose={onClose}
      />,
    );

    expect(addressInput()).toHaveValue("192.168.1.50/24");
  });

  it("submits the complete effective manual form", async () => {
    const { user } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={vi.fn()}
      />,
    );

    await user.clear(addressInput());
    await user.type(addressInput(), "10.0.0.20/24");
    await user.clear(gatewayInput());
    await user.type(gatewayInput(), "10.0.0.1");
    await user.clear(dnsInput());
    await user.type(dnsInput(), "9.9.9.9 1.1.1.1");
    await user.click(
      screen.getByRole("button", { name: "Apply Configuration" }),
    );

    expect(mocks.setIPv4Manual).toHaveBeenCalledWith({
      address: "10.0.0.20/24",
      dns: "9.9.9.9,1.1.1.1",
      gateway: "10.0.0.1",
      iface: "eth0",
    });
  });

  it.each([
    ["enables", 0, "enable_connection", "Enabling connection"],
    ["disables", 100, "disable_connection", "Disabling connection"],
  ] as const)(
    "requests the correct action and blocks duplicate toggles while %s",
    async (_action, state, endpoint, progressLabel) => {
      let pending = false;
      mocks.useCallMutation.mockImplementation(
        (mutationEndpoint: { route: string }) => {
          const mutate = mutationEndpoint.route.endsWith("disable_connection")
            ? mocks.disableConnection
            : mutationEndpoint.route.endsWith("enable_connection")
              ? mocks.enableConnection
              : mutationEndpoint.route.endsWith("set_ipv4_manual")
                ? mocks.setIPv4Manual
                : mocks.setIPv4;
          return {
            isPending: pending && mutationEndpoint.route.endsWith(endpoint),
            mutate,
          };
        },
      );

      const { rerender, user } = render(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface({ state })}
          onClose={vi.fn()}
        />,
      );
      await user.click(screen.getByRole("checkbox"));
      const expectedMutation =
        endpoint === "enable_connection"
          ? mocks.enableConnection
          : mocks.disableConnection;
      expect(expectedMutation).toHaveBeenCalledWith({ iface: "eth0" });

      pending = true;
      rerender(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface({ state })}
          onClose={vi.fn()}
        />,
      );
      expect(
        screen.getByRole("progressbar", { name: progressLabel }),
      ).toBeVisible();
      expect(screen.getByRole("checkbox")).toBeDisabled();
      await user.click(screen.getByRole("checkbox"));
      expect(expectedMutation).toHaveBeenCalledTimes(1);
    },
  );
});
