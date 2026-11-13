import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NetworkInterface } from "@/api";
import { testNetworkInterface } from "@/test/networkInterface";
import { render as renderWithApp, screen, within } from "@/test/render";

import NetworkInterfaceEditor from "./NetworkInterfaceEditor";

const mocks = vi.hoisted(() => ({
  hostname: "10.0.0.9",
  disableConnection: vi.fn(),
  enableConnection: vi.fn(),
  setIPv4: vi.fn(),
  setIPv4Manual: vi.fn(),
  setMTU: vi.fn(),
  setOptional: vi.fn(),
  useCallMutation: vi.fn(),
}));

// jsdom's location cannot be replaced, so the host the browser used is read
// through this one function.
vi.mock("./networkInterfaceState", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./networkInterfaceState")>()),
  getBrowserHostname: () => mocks.hostname,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");

  return {
    ...actual,
    useCallMutation: mocks.useCallMutation,
    linuxio: mockMutationHooks(actual.linuxio, mocks.useCallMutation),
  };
});

const answerMutation = (endpoint: { route: string }) => {
  const mutate = endpoint.route.endsWith("disable_connection")
    ? mocks.disableConnection
    : endpoint.route.endsWith("enable_connection")
      ? mocks.enableConnection
      : endpoint.route.endsWith("set_ipv4_manual")
        ? mocks.setIPv4Manual
        : endpoint.route.endsWith("set_optional")
          ? mocks.setOptional
          : endpoint.route.endsWith("set_mtu")
            ? mocks.setMTU
            : mocks.setIPv4;
  return { isPending: false, mutate };
};

mocks.useCallMutation.mockImplementation(answerMutation);

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

// The editor's controls are for a session with sudo; see the read-only tests.
const render = (ui: ReactElement) =>
  renderWithApp(ui, { auth: { privileged: true } });

const addressInput = () => screen.getByPlaceholderText("192.168.1.10/24");
const gatewayInput = () => screen.getByPlaceholderText("192.168.1.1");
const dnsInput = () => screen.getByPlaceholderText("8.8.8.8, 8.8.4.4");

describe("NetworkInterfaceEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // An earlier test may have left its own pending-state answer behind.
    mocks.useCallMutation.mockImplementation(answerMutation);
    // Not an address of the test interface, so nothing below warns by default.
    mocks.hostname = "10.0.0.9";
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
    expect(mocks.setMTU).not.toHaveBeenCalled();
  });

  it("sends custom DNS and search domains with DHCP when the backend supports them", async () => {
    const { user } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({
          dns_options: { ignore_dhcp: false, search: ["lan"] },
          ipv4_method: "auto",
        })}
        onClose={vi.fn()}
      />,
    );

    const customDns = screen.getByPlaceholderText("Leave empty to use DHCP");
    expect(customDns).toHaveValue("");
    await user.type(customDns, "192.168.1.66");
    await user.type(
      screen.getByPlaceholderText("lan, home.arpa"),
      " home.arpa",
    );
    await user.click(
      screen.getByRole("button", { name: "Apply Configuration" }),
    );

    expect(mocks.setIPv4).toHaveBeenCalledWith({
      dns: "192.168.1.66",
      iface: "eth0",
      method: "dhcp",
      search: "lan,home.arpa",
    });
  });

  it("hides DNS extras when the backend cannot express them", () => {
    render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({ ipv4_method: "auto" })}
        onClose={vi.fn()}
      />,
    );

    expect(screen.queryByPlaceholderText("Leave empty to use DHCP")).toBeNull();
    expect(screen.queryByPlaceholderText("lan, home.arpa")).toBeNull();
    expect(screen.getByPlaceholderText("1500")).toHaveValue(1500);
  });

  it("sets the MTU only when it changed and is in range", async () => {
    const { user } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={vi.fn()}
      />,
    );
    const mtu = screen.getByPlaceholderText("1500");
    const apply = screen.getByRole("button", { name: "Apply Configuration" });

    await user.clear(mtu);
    await user.type(mtu, "40");
    await user.click(apply);
    expect(mocks.setIPv4Manual).not.toHaveBeenCalled();
    expect(mocks.setMTU).not.toHaveBeenCalled();

    await user.clear(mtu);
    await user.type(mtu, "9000");
    await user.click(apply);
    expect(mocks.setMTU).toHaveBeenCalledWith({ iface: "eth0", mtu: "9000" });
    expect(mocks.setIPv4Manual).toHaveBeenCalled();
  });

  it("offers the boot-wait switch only when the backend reports it", async () => {
    const { rerender, user } = render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText("Optional at boot")).toBeNull();

    rerender(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({ optional: false })}
        onClose={vi.fn()}
      />,
    );
    await user.click(screen.getByLabelText("Optional at boot"));

    expect(mocks.setOptional).toHaveBeenCalledWith({
      iface: "eth0",
      optional: true,
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

  describe("when the change may cut this session", () => {
    const browseVia = (hostname: string) => {
      mocks.hostname = hostname;
    };

    it("asks before disabling the interface the browser reaches LinuxIO through", async () => {
      browseVia("192.168.1.25");
      const { user } = render(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface()}
          onClose={vi.fn()}
        />,
      );

      await user.click(screen.getByRole("checkbox"));
      expect(mocks.disableConnection).not.toHaveBeenCalled();
      expect(screen.getByText(/can drop this session/)).toBeInTheDocument();
      expect(screen.getByText(/machine's console/)).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Disable" }));
      expect(mocks.disableConnection).toHaveBeenCalledWith({ iface: "eth0" });
    });

    it("does nothing when the disable warning is cancelled", async () => {
      browseVia("nas.lan");
      const { user } = render(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface()}
          onClose={vi.fn()}
        />,
      );

      await user.click(screen.getByRole("checkbox"));
      await user.click(
        within(screen.getByRole("dialog")).getByRole("button", {
          name: "Cancel",
        }),
      );

      expect(mocks.disableConnection).not.toHaveBeenCalled();
    });

    it("asks before applying IP settings, then applies them", async () => {
      browseVia("192.168.1.25");
      const { user } = render(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface()}
          onClose={vi.fn()}
        />,
      );

      await user.clear(addressInput());
      await user.type(addressInput(), "10.0.0.20/24");
      await user.click(
        screen.getByRole("button", { name: "Apply Configuration" }),
      );
      expect(mocks.setIPv4Manual).not.toHaveBeenCalled();
      expect(screen.getByText(/Applying new IP settings/)).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Apply" }));
      expect(mocks.setIPv4Manual).toHaveBeenCalledWith(
        expect.objectContaining({ address: "10.0.0.20/24", iface: "eth0" }),
      );
    });

    it("acts at once on an interface that is not the one in the address bar", async () => {
      browseVia("10.0.0.9");
      const { user } = render(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface()}
          onClose={vi.fn()}
        />,
      );

      await user.click(screen.getByRole("checkbox"));

      expect(mocks.disableConnection).toHaveBeenCalledWith({ iface: "eth0" });
      expect(screen.queryByText(/can drop this session/)).toBeNull();
    });

    it("acts at once on an interface with no address", async () => {
      browseVia("nas.lan");
      const { user } = render(
        <NetworkInterfaceEditor
          expanded
          iface={manualInterface({ ipv4: [] })}
          onClose={vi.fn()}
        />,
      );

      await user.click(screen.getByRole("checkbox"));

      expect(mocks.disableConnection).toHaveBeenCalledWith({ iface: "eth0" });
    });
  });

  it("shows a session without sudo the settings with nothing to change", () => {
    renderWithApp(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface({
          dns_options: { ignore_dhcp: false, search: ["lan"] },
          mtu: 9000,
          optional: true,
          state: 100,
        })}
        onClose={vi.fn()}
      />,
      { auth: { privileged: false } },
    );

    for (const [label, value] of [
      ["Connection", "Enabled"],
      ["IPv4 method", "Static IP"],
      ["IPv4", "192.168.1.25/24"],
      ["Gateway", "192.168.1.1"],
      ["DNS", "1.1.1.1, 8.8.8.8"],
      ["Search domains", "lan"],
      ["MTU", "9000"],
      ["Optional at boot", "Yes"],
    ]) {
      expect(screen.getByText(label).nextSibling).toHaveTextContent(value);
    }
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByRole("spinbutton")).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(mocks.setIPv4).not.toHaveBeenCalled();
  });

  it("gives a session with sudo the editable form and its save", () => {
    render(
      <NetworkInterfaceEditor
        expanded
        iface={manualInterface()}
        onClose={vi.fn()}
      />,
    );

    expect(addressInput()).toBeEnabled();
    expect(screen.getByRole("checkbox")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Apply Configuration" }),
    ).toBeInTheDocument();
  });
});
