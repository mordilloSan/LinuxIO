import { describe, expect, it } from "vitest";

import {
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
    expect(state.wireguardAvailable).toBeNull();
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
