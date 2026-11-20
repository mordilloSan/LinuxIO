import { describe, expect, it } from "vitest";

import { canonicalHostIP, validateHostIPs } from "@/utils/hostIp";

describe("canonicalHostIP", () => {
  it("keeps a dotted quad and trims it", () => {
    expect(canonicalHostIP(" 192.168.1.66 ")).toBe("192.168.1.66");
    expect(canonicalHostIP("255.255.255.255")).toBe("255.255.255.255");
  });

  it("writes IPv6 the way the backend stores it", () => {
    expect(canonicalHostIP("2001:DB8:0::66")).toBe("2001:db8::66");
    expect(canonicalHostIP("FD00:0:0:0:0:0:0:1")).toBe("fd00::1");
    expect(canonicalHostIP("2001:db8:0:0:1:0:0:1")).toBe("2001:db8::1:0:0:1");
  });

  it("turns an IPv4-mapped IPv6 address into its IPv4 form", () => {
    expect(canonicalHostIP("::ffff:10.0.0.2")).toBe("10.0.0.2");
    expect(canonicalHostIP("::FFFF:c0a8:0142")).toBe("192.168.1.66");
  });

  it("refuses anything that is not an IP address", () => {
    for (const value of [
      "",
      "localhost",
      "192.168.1",
      "192.168.1.256",
      "192.168.01.66",
      "192.168.1.0/24",
      "1.2.3.4; rm -rf /",
      "$(reboot)",
      "fe80::1%eth0",
      "::1]:80/[",
      "1::2::3",
      "12345::1",
      "1.2.3.4\n5.6.7.8",
    ]) {
      expect(canonicalHostIP(value), value).toBeNull();
    }
  });
});

describe("validateHostIPs", () => {
  it("returns the canonical value of every valid row", () => {
    expect(
      validateHostIPs(["192.168.1.66", " 2001:DB8::66 ", "::ffff:10.0.0.2"]),
    ).toEqual({
      hostIPs: ["192.168.1.66", "2001:db8::66", "10.0.0.2"],
      errors: ["", "", ""],
    });
  });

  it("accepts an empty list", () => {
    expect(validateHostIPs([])).toEqual({ hostIPs: [], errors: [] });
  });

  it("names what is wrong with each row", () => {
    const { hostIPs, errors } = validateHostIPs([
      "  ",
      "nas.local",
      "fe80::1%eth0",
      "0.0.0.0",
      "::",
      "127.0.0.1",
      "::1",
      "::ffff:127.0.0.1",
      "10.0.0.2",
    ]);

    expect(errors).toEqual([
      "IP address is required.",
      "Enter an IPv4 or IPv6 address.",
      "An address with a zone (%) cannot be used.",
      "Enter a specific address, not the one that means every address.",
      "Enter a specific address, not the one that means every address.",
      "A loopback address is always assigned.",
      "A loopback address is always assigned.",
      "A loopback address is always assigned.",
      "",
    ]);
    expect(hostIPs.at(-1)).toBe("10.0.0.2");
    expect(hostIPs[1]).toBe("");
  });

  it("marks the later row of a duplicate, whatever its spelling", () => {
    expect(
      validateHostIPs(["2001:DB8::1", "2001:db8:0::1", "10.0.0.2"]).errors,
    ).toEqual(["", "IP address is already listed.", ""]);
    expect(validateHostIPs(["10.0.0.2", "::ffff:10.0.0.2"]).errors).toEqual([
      "",
      "IP address is already listed.",
    ]);
  });
});
