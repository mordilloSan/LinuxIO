import { describe, expect, it } from "vitest";

import type { NetworkInterface } from "@/api";
import { testNetworkInterface } from "@/test/networkInterface";

import { getEgressNICOptions } from "./wireguardEgressNICs";

const networkInterface = (
  overrides: Partial<NetworkInterface>,
): NetworkInterface =>
  testNetworkInterface({ name: "enp1s0", speed: "1 Gbps", ...overrides });

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
