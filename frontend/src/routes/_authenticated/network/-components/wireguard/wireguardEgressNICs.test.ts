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
      { name: "eth0", label: "eth0 (no IPv4 address)" },
    ]);
  });

  it("excludes virtual, loopback, and addressless interfaces", () => {
    const options = getEgressNICOptions([
      networkInterface({ name: "veth1234" }),
      networkInterface({ name: "docker0" }),
      networkInterface({ name: "br-app" }),
      networkInterface({ ipv4: ["192.168.122.1/24"], name: "virbr0" }),
      networkInterface({ name: "vnet0" }),
      networkInterface({ name: "lo", type: "loopback" }),
      networkInterface({ mac: "", name: "enp2s0" }),
    ]);

    expect(options).toEqual([]);
  });

  it("puts the card holding the gateway first, a host bridge included", () => {
    // The LAN card is a port of br-enp2s0, which holds the address and the
    // default route; libvirt's NAT bridge and the VM's tap are not ways out.
    const options = getEgressNICOptions([
      networkInterface({ name: "enp2s0" }),
      networkInterface({
        gateway: "192.168.1.1",
        ipv4: ["192.168.1.66/24"],
        name: "br-enp2s0",
      }),
      networkInterface({ ipv4: ["192.168.122.1/24"], name: "virbr0" }),
      networkInterface({ name: "vnet0" }),
      networkInterface({ ipv4: ["10.0.0.5/24"], name: "enp3s0" }),
    ]);

    expect(options).toEqual([
      { name: "br-enp2s0", label: "br-enp2s0 (192.168.1.66/24)" },
      { name: "enp3s0", label: "enp3s0 (10.0.0.5/24)" },
      { name: "enp2s0", label: "enp2s0 (no IPv4 address)" },
    ]);
  });
});
