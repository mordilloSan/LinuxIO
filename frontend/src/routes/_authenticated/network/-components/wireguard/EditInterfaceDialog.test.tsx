import { afterEach, describe, expect, it, vi } from "vitest";

import type { WireGuardInterface } from "@/api";
import * as core from "@/api/linuxio-core";
import { testNetworkInterface } from "@/test/networkInterface";
import { render, screen, waitFor } from "@/test/render";

import EditInterfaceDialog from "./EditInterfaceDialog";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

const wg0: WireGuardInterface = {
  address: "10.0.0.1/24",
  dns: ["1.1.1.1"],
  hooks: {
    PreUp: [],
    PostUp: [
      "sysctl -w net.ipv4.ip_forward=1",
      "iptables -A FORWARD -i wg0 -j ACCEPT",
    ],
    PreDown: [],
    PostDown: ["iptables -D FORWARD -i wg0 -j ACCEPT || true"],
  },
  host: "",
  egressNic: "eth0",
  forwardingOff: false,
  isConnected: "Active",
  isEnabled: true,
  mtu: 0,
  name: "wg0",
  peerCount: 1,
  port: 51820,
};

function spyUpdates() {
  vi.spyOn(core, "request").mockImplementation((_handler, command) =>
    command === "get_network_info"
      ? Promise.resolve([
          testNetworkInterface({ ipv4: ["10.1.0.2/24"], name: "eth0" }),
          testNetworkInterface({
            gateway: "192.168.1.1",
            ipv4: ["192.168.1.66/24"],
            name: "br-lan",
          }),
        ])
      : Promise.resolve(),
  );
  const updates: unknown[] = [];
  vi.spyOn(core, "mutationRequest").mockImplementation(
    (_handler, command, request) => {
      if (command === "update_interface") updates.push(request);
      return Promise.resolve();
    },
  );
  return updates;
}

describe("EditInterfaceDialog", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("sends only the fields that changed and explains the restart", async () => {
    const updates = spyUpdates();
    const onClose = vi.fn();
    const view = render(
      <EditInterfaceDialog iface={wg0} onClose={onClose} open />,
    );

    await waitFor(() =>
      expect(
        screen.getByText(
          /Changing the address, NIC, port, MTU or hooks restarts wg0/,
        ),
      ).toBeVisible(),
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await view.user.type(
      screen.getByLabelText("Endpoint host"),
      "vpn.example.org",
    );
    const port = screen.getByLabelText("Port");
    await view.user.clear(port);
    await view.user.type(port, "51830");
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({
      name: "wg0",
      host: "vpn.example.org",
      listenPort: 51830,
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("sends hook fields as one command per line", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditInterfaceDialog iface={wg0} onClose={vi.fn()} open />,
    );

    const preUp = screen.getByLabelText("PreUp");
    await view.user.type(preUp, "echo one{enter}{enter}  echo two  ");
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({
      name: "wg0",
      hooks: { PreUp: ["echo one", "echo two"] },
    });
  });

  it("shows each hook kind in file order and sends an empty list to remove one", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditInterfaceDialog iface={wg0} onClose={vi.fn()} open />,
    );

    const hookFields = screen.getAllByRole("textbox", {
      name: /^(Pre|Post)(Up|Down)$/,
    });
    expect(hookFields).toEqual(
      ["PreUp", "PostUp", "PreDown", "PostDown"].map((label) =>
        screen.getByLabelText(label),
      ),
    );
    expect(screen.getByLabelText("PostUp")).toHaveValue(
      "sysctl -w net.ipv4.ip_forward=1\niptables -A FORWARD -i wg0 -j ACCEPT",
    );

    await view.user.clear(screen.getByLabelText("PostDown"));
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({ name: "wg0", hooks: { PostDown: [] } });
  });

  it("blocks saving while the port or MTU is invalid", async () => {
    spyUpdates();
    const view = render(
      <EditInterfaceDialog iface={wg0} onClose={vi.fn()} open />,
    );

    const port = screen.getByLabelText("Port");
    await view.user.clear(port);
    await view.user.type(port, "70000");
    expect(screen.getByText("Use 1 to 65535")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await view.user.clear(port);
    await view.user.type(port, "51821");
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();

    const mtu = screen.getByLabelText("MTU");
    await view.user.type(mtu, "1000");
    expect(screen.getByText("Use 0 or 1280 to 65535")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("moves the address and says every device must import its configuration again", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditInterfaceDialog iface={wg0} onClose={vi.fn()} open />,
    );

    const address = screen.getByLabelText("Address");
    expect(address).toHaveValue("10.0.0.1/24");
    expect(screen.queryByText(/Every device gets a new address/)).toBeNull();

    await view.user.clear(address);
    await view.user.type(address, "10.0.5.5/24");
    expect(
      screen.getByText(/Use the first address of a \/24 to \/30 subnet/),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await view.user.clear(address);
    await view.user.type(address, "10.0.5.0/24");
    expect(screen.getByText(/Every device gets a new address/)).toBeVisible();
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({ name: "wg0", address: "10.0.5.0/24" });
  });

  it("moves the NAT lines to another network card", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditInterfaceDialog iface={wg0} onClose={vi.fn()} open />,
    );

    const nic = screen.getByRole("combobox", { name: "NIC" });
    await waitFor(() => expect(nic).toHaveTextContent("eth0 (10.1.0.2/24)"));
    await view.user.click(nic);
    await view.user.click(
      await screen.findByRole("option", { name: "br-lan (192.168.1.66/24)" }),
    );
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({ name: "wg0", egressNic: "br-lan" });
  });

  it("offers no NIC where the hooks hold no NAT lines of LinuxIO's", () => {
    spyUpdates();
    render(
      <EditInterfaceDialog
        iface={{ ...wg0, egressNic: "" }}
        onClose={vi.fn()}
        open
      />,
    );

    expect(screen.queryByRole("combobox", { name: "NIC" })).toBeNull();
  });
});
