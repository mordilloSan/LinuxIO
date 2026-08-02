import { describe, expect, it, vi } from "vitest";

import type { NetworkInterface, WireGuardInterface } from "@/api";
import DockerStatCard from "@/components/cards/DockerStatCard";
import DriveCard from "@/components/cards/DriveCard";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import { render, screen } from "@/test/render";

vi.mock("@iconify/react", () => ({
  Icon: () => <span aria-hidden="true" />,
}));

const networkInterface: NetworkInterface = {
  dns: [],
  duplex: "full",
  gateway: "",
  ipv4: ["192.0.2.1"],
  ipv6: [],
  mac: "00:00:00:00:00:01",
  mtu: 1500,
  name: "eth0",
  rx_speed: 0,
  speed: "1000",
  state: 100,
  tx_speed: 0,
  type: "ethernet",
};

const wireguardInterface: WireGuardInterface = {
  address: "10.0.0.1/24",
  isConnected: "Active",
  isEnabled: true,
  name: "wg0",
  peerCount: 1,
  port: 51820,
};

describe("interactive cards", () => {
  it("provides a native keyboard-activatable Docker stat trigger", async () => {
    const onClick = vi.fn();
    const { user } = render(
      <DockerStatCard
        detail="2 running"
        label="Containers"
        onClick={onClick}
        value={2}
      />,
    );

    const trigger = screen.getByRole("button", {
      name: /Containers.*2 running/i,
    });
    trigger.focus();
    await user.keyboard("{Enter}");
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("keeps WireGuard icon actions separate from the card trigger", async () => {
    const onSelect = vi.fn();
    const onToggle = vi.fn();
    const { user } = render(
      <WireguardInterfaceCard
        handleAddPeer={vi.fn()}
        handleDelete={vi.fn()}
        handleSelectInterface={onSelect}
        handleToggleBootPersistence={vi.fn()}
        handleToggleInterface={onToggle}
        iface={wireguardInterface}
        selectedCardRef={null}
        selectedInterface={null}
      />,
    );

    await user.click(
      screen.getByRole("button", { name: "Turn interface off" }),
    );
    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("exposes the drive summary as an expandable keyboard trigger", async () => {
    const onClick = vi.fn();
    const { user } = render(
      <DriveCard
        expanded={false}
        model="Example SSD"
        name="sda"
        onClick={onClick}
        sizeBytes={1024}
        transport="sata"
      />,
    );

    const trigger = screen.getByRole("button", {
      name: /\/dev\/sda.*Example SSD/i,
    });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    trigger.focus();
    await user.keyboard("{Enter}");
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("supports keyboard activation for Network interface expansion", async () => {
    const onToggle = vi.fn();
    const { user } = render(
      <NetworkInterfaceCard
        expanded={false}
        iface={networkInterface}
        onClose={vi.fn()}
        onToggle={onToggle}
      />,
    );

    const trigger = screen.getByRole("button", { name: /eth0.*IPv4/i });
    trigger.focus();
    await user.keyboard(" ");
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
