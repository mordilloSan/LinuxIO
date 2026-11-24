import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import type { NetworkInterface } from "@/api";
import { testNetworkInterface } from "@/test/networkInterface";
import { render, screen } from "@/test/render";

import NetworkInterfacesCard from "./Network";

const mocks = vi.hoisted(() => ({
  interfaces: [] as NetworkInterface[],
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: ({
      select,
    }: {
      select?: (interfaces: NetworkInterface[]) => unknown;
    }) => ({
      data: select ? select(mocks.interfaces) : mocks.interfaces,
    }),
  };
});

vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
  CardHeaderSelect: () => null,
  CardStatusDot: () => null,
  default: ({ stats }: { stats: ReactNode }) => <section>{stats}</section>,
}));

vi.mock("./DashboardStatRows", () => ({
  default: ({ rows }: { rows: Array<{ label: string; value: string }> }) => (
    <dl>
      {rows.map((row) => (
        <div key={row.label}>
          <dt>{row.label}</dt>
          <dd>{row.value}</dd>
        </div>
      ))}
    </dl>
  ),
}));

const valueOf = (label: string) =>
  screen.getByText(label).nextElementSibling?.textContent;

describe("NetworkInterfacesCard", () => {
  it("shows a bridge port's address from the bridge it belongs to", () => {
    mocks.interfaces = [
      testNetworkInterface({
        name: "br-enp2s0",
        ipv4: ["192.168.1.66/24"],
      }),
      testNetworkInterface({ name: "enp2s0", master: "br-enp2s0" }),
    ];

    render(<NetworkInterfacesCard />);

    expect(valueOf("IPv4")).toBe("192.168.1.66/24");
    expect(valueOf("Port of")).toBe("br-enp2s0");
  });

  it("says None when the bridge has no address either", () => {
    mocks.interfaces = [
      testNetworkInterface({ name: "br-lan" }),
      testNetworkInterface({ name: "enp3s0", master: "br-lan" }),
    ];

    render(<NetworkInterfacesCard />);

    expect(valueOf("IPv4")).toBe("None");
    expect(valueOf("Port of")).toBe("br-lan");
  });

  it("shows a card's own address and no bridge when it stands alone", () => {
    mocks.interfaces = [
      testNetworkInterface({ name: "eth0", ipv4: ["10.0.0.2/24"] }),
    ];

    render(<NetworkInterfacesCard />);

    expect(valueOf("IPv4")).toBe("10.0.0.2/24");
    expect(screen.queryByText("Port of")).not.toBeInTheDocument();
  });
});
