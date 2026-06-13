import { describe, expect, it } from "vitest";

import {
  getCapabilityReason,
  getCapabilityStatus,
  hasAccessPolicy,
  isCapabilityEnabled,
} from "@/hooks/useCapabilities";
import { emptyCapabilityState } from "@/api/capabilities";

describe("capability access helpers", () => {
  it("normalizes capability status", () => {
    expect(getCapabilityStatus(true)).toBe("available");
    expect(getCapabilityStatus(false)).toBe("unavailable");
    expect(getCapabilityStatus(null)).toBe("unknown");
    expect(isCapabilityEnabled(true)).toBe(true);
    expect(isCapabilityEnabled(false)).toBe(false);
  });

  it("evaluates access policy requirements", () => {
    const access = {
      ...emptyCapabilityState,
      dockerAvailable: true,
      privileged: false,
    };

    expect(hasAccessPolicy(undefined, access)).toBe(true);
    expect(
      hasAccessPolicy({ requiredCapabilities: ["dockerAvailable"] }, access),
    ).toBe(true);
    expect(
      hasAccessPolicy({ requiredCapabilities: ["wireguardAvailable"] }, access),
    ).toBe(false);
    expect(hasAccessPolicy({ requiresPrivileged: true }, access)).toBe(false);
  });

  it("returns configured reason text", () => {
    expect(getCapabilityReason("dockerAvailable", "available")).toBe("");
    expect(getCapabilityReason("dockerAvailable", "unknown")).toMatch(
      /still being checked/i,
    );
    expect(getCapabilityReason("dockerAvailable", "unavailable")).toMatch(
      /unavailable/i,
    );
  });
});
