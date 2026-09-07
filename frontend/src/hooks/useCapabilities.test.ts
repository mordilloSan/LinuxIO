import { QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import {
  getCapabilityReason,
  getCapabilityStatus,
  hasAccessPolicy,
  isCapabilityEnabled,
  useCapabilitiesResponse,
  useCapabilityState,
} from "@/hooks/useCapabilities";
import { createTestQueryClient, renderHook } from "@/test/render";

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

  it("subscribes to the capability cache with a query function", () => {
    const consoleError = vi.spyOn(console, "error");
    const client = createTestQueryClient();
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client }, children);

    renderHook(
      () => ({ state: useCapabilityState(), wire: useCapabilitiesResponse() }),
      { wrapper },
    );

    expect(consoleError).not.toHaveBeenCalledWith(
      expect.stringContaining("No queryFn was passed"),
    );
  });
});
