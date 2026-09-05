import { getIcon } from "@iconify/react";
import { describe, expect, it } from "vitest";

import { CAPABILITIES } from "@/api/capabilities";
import { distroIcons, getDistroIcon } from "@/icons/distro";
import "@/icons/icons";
import "@/icons/shell";

describe("bundled distro and WireGuard icons", () => {
  it("resolves distro aliases and unknown platforms locally", () => {
    expect(getDistroIcon("ubuntu")).toBe(distroIcons.ubuntu);
    expect(getDistroIcon("alpine")).toBe(distroIcons.alpine);
    expect(getDistroIcon("opensuse-tumbleweed")).toBe(distroIcons.opensuse);
    for (const platform of ["", "unknown", "toString", "__proto__"]) {
      expect(getDistroIcon(platform)).toBe(getDistroIcon("linux"));
    }
  });

  it("registers every selected logo without needing an API lookup", () => {
    const wireguard = CAPABILITIES.find((item) => item.wire === "wireguard");
    expect(wireguard).toBeDefined();
    for (const icon of [
      ...Object.values(distroIcons),
      getDistroIcon("linux"),
      wireguard!.icon,
    ]) {
      expect(getIcon(icon)?.body, icon).toBeTruthy();
    }
  });
});
