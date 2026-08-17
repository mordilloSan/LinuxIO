import { describe, expect, it } from "vitest";

import type { NetworkInterface } from "@/api";

import { getEgressNICOptions } from "./wireguardEgressNICs";

const networkInterface = (
  overrides: Partial<NetworkInterface>,
): NetworkInterface => ({
  dns: [],
  duplex: "full",
  gateway: "",
  ipv4: [],
  mac: "00:11:22:33:44:55",
  name: "enp1s0",
  rx_speed: 0,
  speed: "1 Gbps",
  state: 100,
  tx_speed: 0,
  type: "ethernet",
  ...overrides,
});

describe("getEgressNICOptions", () => {
  it("includes wireless and conventionally named Ethernet interfaces", () => {
    const options = getEgressNICOptions([
      networkInterface({
        ipv4: ["192.168.1.20/24"],
        name: "wlp2s0",
        type: "wifi",
      }),
      networkInterface({ name: "eth0" }),
    ]);

    expect(options).toEqual([
      { name: "wlp2s0", label: "wlp2s0 (192.168.1.20/24)" },
      { name: "eth0", label: "eth0 (disconnected)" },
    ]);
  });

  it("excludes virtual, loopback, and addressless interfaces", () => {
    const options = getEgressNICOptions([
      networkInterface({ name: "veth1234" }),
      networkInterface({ name: "docker0" }),
      networkInterface({ name: "br-app" }),
      networkInterface({ name: "lo", type: "loopback" }),
      networkInterface({ mac: "", name: "enp2s0" }),
    ]);

    expect(options).toEqual([]);
  });
});
