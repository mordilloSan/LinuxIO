import { afterEach, describe, expect, it, vi } from "vitest";

import type { Peer } from "@/api";
import * as core from "@/api/linuxio-core";
import { render, screen, waitFor } from "@/test/render";

import EditPeerDialog from "./EditPeerDialog";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

const alice: Peer = {
  address: "10.0.0.2/32",
  client_allowed_ips: ["0.0.0.0/0", "::/0"],
  dns: ["1.1.1.1"],
  enabled: true,
  id: "Peer2",
  mtu: 0,
  name: "Alice",
  persistent_keepalive: 25,
  preshared_key: "psk",
  public_key: "alice-key",
  server_allowed_ips: [],
};

function spyUpdates() {
  const updates: unknown[] = [];
  vi.spyOn(core, "request").mockImplementation((_handler, command, request) => {
    if (command === "update_peer") updates.push(request);
    return Promise.resolve();
  });
  return updates;
}

describe("EditPeerDialog", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("sends only the fields that changed", async () => {
    const updates = spyUpdates();
    const onClose = vi.fn();
    const view = render(
      <EditPeerDialog
        interfaceName="wg0"
        onClose={onClose}
        open
        peer={alice}
      />,
    );

    const name = screen.getByLabelText("Name");
    await view.user.clear(name);
    await view.user.type(name, "Alice's phone");
    const mtu = screen.getByLabelText("MTU");
    await view.user.clear(mtu);
    await view.user.type(mtu, "1400");
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({
      interfaceName: "wg0",
      peerId: "Peer2",
      name: "Alice's phone",
      mtu: 1400,
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("blocks saving while a value is invalid", async () => {
    spyUpdates();
    const view = render(
      <EditPeerDialog
        interfaceName="wg0"
        onClose={vi.fn()}
        open
        peer={alice}
      />,
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    const mtu = screen.getByLabelText("MTU");
    await view.user.clear(mtu);
    await view.user.type(mtu, "1000");
    expect(screen.getByText("Use 0 or 1280 to 65535")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    await view.user.clear(mtu);
    await view.user.type(mtu, "1400");
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();

    const routes = screen.getByLabelText("Client routes");
    await view.user.clear(routes);
    await view.user.type(routes, "10.0.0.1");
    expect(screen.getByText("Enter comma-separated CIDRs")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("resets client routes with the full tunnel preset", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditPeerDialog
        interfaceName="wg0"
        onClose={vi.fn()}
        open
        peer={{ ...alice, client_allowed_ips: ["10.0.0.0/24"] }}
      />,
    );

    await view.user.click(screen.getByRole("button", { name: "Full tunnel" }));
    expect(screen.getByLabelText("Client routes")).toHaveValue(
      "0.0.0.0/0, ::/0",
    );
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({
      interfaceName: "wg0",
      peerId: "Peer2",
      clientAllowedIPs: ["0.0.0.0/0", "::/0"],
    });
  });

  it("turning the preshared key off sends a remove action", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditPeerDialog
        interfaceName="wg0"
        onClose={vi.fn()}
        open
        peer={alice}
      />,
    );

    await view.user.click(
      screen.getByRole("switch", { name: "Preshared key" }),
    );
    await view.user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({
      interfaceName: "wg0",
      peerId: "Peer2",
      presharedKey: "remove",
    });
  });

  it("regenerates keys only after confirmation", async () => {
    const updates = spyUpdates();
    const view = render(
      <EditPeerDialog
        interfaceName="wg0"
        onClose={vi.fn()}
        open
        peer={alice}
      />,
    );

    await view.user.click(
      screen.getByRole("button", { name: "Regenerate keys" }),
    );
    expect(updates).toHaveLength(0);
    await waitFor(() =>
      expect(screen.getByText("Regenerate keys?")).toBeVisible(),
    );

    await view.user.click(screen.getByRole("button", { name: "Regenerate" }));
    await waitFor(() => expect(updates).toHaveLength(1));
    expect(updates[0]).toEqual({
      interfaceName: "wg0",
      peerId: "Peer2",
      regenerateKeys: true,
    });
  });
});
