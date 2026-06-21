import { describe, expect, it } from "vitest";

import {
  CAPABILITIES,
  CAPABILITY_KEYS,
  capabilityStateFromWire,
  emptyCapabilityState,
  parseCapabilityState,
  pickCapabilityState,
} from "@/api/capabilities";

describe("capabilities helpers", () => {
  it("maps wire booleans into tri-state frontend capability state", () => {
    const state = capabilityStateFromWire({
      docker_available: true,
      packagekit_available: false,
    });

    expect(state.dockerAvailable).toBe(true);
    expect(state.packageKitAvailable).toBe(false);
    expect(state.libvirtAvailable).toBeNull();
    expect(state.wireguardAvailable).toBeNull();
  });

  it("defines libvirt as an installable routed capability", () => {
    const libvirt = CAPABILITIES.find(
      (capability) => capability.wire === "libvirt",
    );

    expect(libvirt).toMatchObject({
      dependency: "libvirt",
      installable: { requiresPackageKit: true },
      route: { href: "/vm", label: "Open VMs" },
      state: "libvirtAvailable",
    });
  });

  it("parses untrusted capability JSON safely", () => {
    expect(parseCapabilityState(null)).toEqual(emptyCapabilityState);
    expect(
      parseCapabilityState({
        dockerAvailable: true,
        packageKitAvailable: "yes",
      }),
    ).toMatchObject({
      dockerAvailable: true,
      packageKitAvailable: null,
    });
  });

  it("picks only known capability fields", () => {
    const picked = pickCapabilityState({
      dockerAvailable: true,
      wireguardAvailable: false,
    });

    expect(picked.dockerAvailable).toBe(true);
    expect(picked.wireguardAvailable).toBe(false);
    expect(Object.keys(picked).sort()).toEqual([...CAPABILITY_KEYS].sort());
  });
});
