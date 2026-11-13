import { describe, expect, it } from "vitest";

import {
  getNetworkCarrierLabel,
  getNetworkStateColor,
  getNetworkStateLabel,
  getNetworkStateSummary,
  mayCarryBrowserSession,
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

describe("mayCarryBrowserSession", () => {
  const eth = { ipv4: ["192.168.1.25/24"], state: 100 };

  it("matches an IPv4 hostname against the interface addresses", () => {
    expect(mayCarryBrowserSession(eth, "192.168.1.25")).toBe(true);
    expect(mayCarryBrowserSession(eth, "10.0.0.9")).toBe(false);
  });

  it("treats a connected interface with an address as possibly the one for a name", () => {
    expect(mayCarryBrowserSession(eth, "nas.lan")).toBe(true);
    expect(mayCarryBrowserSession(eth, "[fe80::1]")).toBe(true);
  });

  it("is plainly not an interface that is down or has no address", () => {
    expect(mayCarryBrowserSession({ ...eth, state: 30 }, "nas.lan")).toBe(
      false,
    );
    expect(mayCarryBrowserSession({ ipv4: [], state: 100 }, "nas.lan")).toBe(
      false,
    );
  });

  it("never crosses an interface for a loopback hostname", () => {
    expect(mayCarryBrowserSession(eth, "localhost")).toBe(false);
    expect(mayCarryBrowserSession(eth, "127.0.0.1")).toBe(false);
    expect(mayCarryBrowserSession(eth, "[::1]")).toBe(false);
  });
});
