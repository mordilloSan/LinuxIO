import { describe, expect, it } from "vitest";

import {
  getNetworkCarrierLabel,
  getNetworkStateColor,
  getNetworkStateLabel,
  getNetworkStateSummary,
} from "./networkInterfaceState";

describe("network interface state", () => {
  it("names every device state the bridge can report", () => {
    expect(getNetworkStateLabel(100)).toBe("Connected");
    expect(getNetworkStateLabel(110)).toBe("Deactivating");
    expect(getNetworkStateLabel(70)).toBe("Connecting");
    expect(getNetworkStateLabel(30)).toBe("Disconnected");
    expect(getNetworkStateLabel(20)).toBe("Unavailable");
    expect(getNetworkStateLabel(120)).toBe("Failed");
    expect(getNetworkStateLabel(10)).toBe("Unmanaged");
    expect(getNetworkStateLabel(0)).toBe("Unknown");
  });

  it("colours connected, in-flight and failed states apart", () => {
    expect(getNetworkStateColor(100)).toBe("var(--app-palette-success-main)");
    expect(getNetworkStateColor(70)).toBe("var(--app-palette-warning-main)");
    expect(getNetworkStateColor(120)).toBe("var(--app-palette-error-main)");
    expect(getNetworkStateColor(10)).toBe("var(--app-palette-text-disabled)");
  });

  it("keeps an absent carrier reading distinct from a down one", () => {
    expect(getNetworkCarrierLabel(undefined)).toBe("unknown");
    expect(getNetworkCarrierLabel(false)).toBe("down");
    expect(getNetworkCarrierLabel(true)).toBe("connected");
  });

  it("summarises the readings the statistics card no longer lists", () => {
    expect(
      getNetworkStateSummary({ carrier: true, operstate: "up", state: 100 }),
    ).toBe("Connected · carrier connected · link up");
    expect(getNetworkStateSummary({ operstate: "down", state: 20 })).toBe(
      "Unavailable · carrier unknown · link down",
    );
  });
});
