import { describe, expect, expectTypeOf, it } from "vitest";

import {
  CAPABILITIES,
  CAPABILITY_KEYS,
  type CapabilityWire,
  capabilityStateFromWire,
  emptyCapabilityState,
  parseCapabilityState,
  pickCapabilityState,
  wireFromCapabilityState,
} from "@/api/capabilities";
import type { CapabilityName } from "@/api/generated/linuxio-types";

describe("capabilities helpers", () => {
  it("names every backend capability exactly once", () => {
    // Typecheck fails when the table and the backend registry's generated
    // CapabilityName union differ in either direction.
    expectTypeOf<CapabilityWire>().toEqualTypeOf<CapabilityName>();

    const wires = CAPABILITIES.map((capability) => capability.wire);
    const states = CAPABILITIES.map((capability) => capability.state);
    expect(new Set(wires).size).toBe(wires.length);
    expect(new Set(states).size).toBe(states.length);
  });

  it("maps wire statuses into tri-state frontend capability state", () => {
    const state = capabilityStateFromWire({
      docker: { available: true },
      packagekit: { available: false, error: "PackageKit is not running" },
      rsync: { available: true },
    });

    expect(state.dockerAvailable).toBe(true);
    expect(state.packageKitAvailable).toBe(false);
    expect(state.rsyncAvailable).toBe(true);
    expect(state.libvirtAvailable).toBeNull();
    expect(state.wireguardAvailable).toBeNull();
  });

  it("seeds the wire shape from known state only", () => {
    expect(
      wireFromCapabilityState({
        dockerAvailable: true,
        rsyncAvailable: false,
        libvirtAvailable: null,
      }),
    ).toEqual({
      docker: { available: true },
      rsync: { available: false },
    });
  });

  it("defines libvirt as an installable routed capability", () => {
    const libvirt = CAPABILITIES.find(
      (capability) => capability.wire === "libvirt",
    );

    expect(libvirt).toMatchObject({
      dependency: "libvirt",
      installable: { requiresPackageKit: true },
      route: { label: "Open VMs", to: "/vm" },
      state: "libvirtAvailable",
    });
  });

  it("defines monitoring as a built-in component", () => {
    const monitoring = CAPABILITIES.find(
      (capability) => capability.wire === "monitoring",
    );

    expect(monitoring).toMatchObject({
      dependency: "linuxio-monitoring",
      state: "monitoringAvailable",
    });
    // Built-in components have no install path, so the key is absent
    // (CapabilityManagerSection reads it with the same `in` check).
    expect(monitoring && "installable" in monitoring).toBe(false);
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
