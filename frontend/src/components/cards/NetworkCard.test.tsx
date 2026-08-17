import { describe, expect, it, vi } from "vitest";

import type { DockerNetwork } from "@/api";
import { render, screen } from "@/test/render";

import NetworkCard from "./NetworkCard";

const network: DockerNetwork = {
  Attachable: false,
  ConfigOnly: false,
  Driver: "bridge",
  EnableIPv4: true,
  EnableIPv6: false,
  Id: "network-id",
  Ingress: false,
  Internal: false,
  IPAM: { Config: [{ Gateway: "172.18.0.1", Subnet: "172.18.0.0/16" }] },
  Name: "example-network",
  Scope: "local",
};

describe("NetworkCard", () => {
  it("uses double click selection without a checkbox", async () => {
    const onSelect = vi.fn();
    const { rerender, user } = render(
      <NetworkCard network={network} onSelect={onSelect} selected={false} />,
    );

    const card = screen.getByRole("button", {
      name: `Select network ${network.Name}`,
    });
    expect(card).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("checkbox")).toBeNull();

    await user.click(card);
    expect(onSelect).not.toHaveBeenCalled();

    await user.dblClick(card);
    expect(onSelect).toHaveBeenCalledWith(true);
    expect(card).not.toHaveFocus();

    rerender(<NetworkCard network={network} onSelect={onSelect} selected />);
    const selectedCard = screen.getByRole("button", {
      name: `Deselect network ${network.Name}`,
    });
    expect(selectedCard).toHaveAttribute("aria-pressed", "true");

    selectedCard.focus();
    await user.keyboard(" ");
    expect(onSelect).toHaveBeenLastCalledWith(false);
    expect(selectedCard).toHaveFocus();
  });
});
