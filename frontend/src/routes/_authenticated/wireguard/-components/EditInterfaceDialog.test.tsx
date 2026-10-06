import { afterEach, describe, expect, it, vi } from "vitest";

import type { WireGuardInterface } from "@/api";
import * as core from "@/api/linuxio-core";
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
  host: "",
  isConnected: "Active",
  isEnabled: true,
  mtu: 0,
  name: "wg0",
  peerCount: 1,
  port: 51820,
  postDown: ["iptables -D FORWARD -i wg0 -j ACCEPT || true"],
  postUp: [
    "sysctl -w net.ipv4.ip_forward=1",
    "iptables -A FORWARD -i wg0 -j ACCEPT",
  ],
  preDown: [],
  preUp: [],
};

function spyUpdates() {
  const updates: unknown[] = [];
  vi.spyOn(core, "request").mockImplementation((_handler, command, request) => {
    if (command === "update_interface") updates.push(request);
    return Promise.resolve();
  });
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
        screen.getByText(/Changing port, MTU or hooks restarts wg0/),
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
      preUp: ["echo one", "echo two"],
    });
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
});
