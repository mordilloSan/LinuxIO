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
  it("opens on one click and keyboard activation", async () => {
    const onOpen = vi.fn();
    const { user } = render(
      <NetworkCard network={network} onOpen={onOpen} selected={false} />,
    );

    const card = screen.getByRole("button", {
      name: `Open network ${network.Name} details`,
    });
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(
      screen.getByRole("heading", { name: network.Name }),
    ).toBeInTheDocument();
    expect(screen.getByText("bridge · local")).toBeInTheDocument();

    await user.click(card);
    expect(onOpen).toHaveBeenCalledTimes(1);
    await user.keyboard(" ");
    expect(onOpen).toHaveBeenCalledTimes(2);
  });
});
