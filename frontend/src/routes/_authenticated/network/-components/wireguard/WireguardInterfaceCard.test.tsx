import { describe, expect, it, vi } from "vitest";

import type { WireGuardInterface } from "@/api";
import { render, screen } from "@/test/render";

import WireguardInterfaceCard from "./WireguardInterfaceCard";

const wg0: WireGuardInterface = {
  address: "10.0.0.1/24",
  dns: [],
  forwardingOff: false,
  hooks: { PreUp: [], PostUp: [], PreDown: [], PostDown: [] },
  host: "",
  isConnected: "Active",
  isEnabled: true,
  mtu: 0,
  name: "wg0",
  peerCount: 1,
  port: 51820,
};

const renderCard = (iface: WireGuardInterface) => {
  const onRestore = vi.fn();
  const onSelect = vi.fn();
  const view = render(
    <WireguardInterfaceCard
      handleAddPeer={vi.fn()}
      handleDelete={vi.fn()}
      handleEdit={vi.fn()}
      handleRestoreForwarding={onRestore}
      handleSelectInterface={onSelect}
      handleToggleBootPersistence={vi.fn()}
      handleToggleInterface={vi.fn()}
      iface={iface}
      selectedCardRef={null}
      selectedInterface={null}
    />,
  );
  return { ...view, onRestore, onSelect };
};

// An interface that is up while the server forwards nothing looks healthy
// from every other sign: its peers connect and handshake.
describe("WireGuard interface card, forwarding", () => {
  it("says so and offers the fix when an interface that is up does not forward", async () => {
    const { user, onRestore, onSelect } = renderCard({
      ...wg0,
      forwardingOff: true,
    });

    expect(screen.getByText("Not forwarding")).toBeInTheDocument();
    expect(screen.queryByText("Active")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Peers can't reach your network",
    );

    await user.click(
      screen.getByRole("button", { name: "Turn on forwarding" }),
    );
    expect(onRestore).toHaveBeenCalledWith("wg0");
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("shows no warning for an interface that forwards", () => {
    renderCard(wg0);

    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows no warning for an interface that is down", () => {
    renderCard({ ...wg0, forwardingOff: true, isConnected: "Inactive" });

    expect(screen.getByText("Inactive")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
