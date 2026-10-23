import { getIcon } from "@iconify/react";
import { describe, expect, it } from "vitest";

import "@/icons/icons";
import "@/icons/shell";

import { vmOSIcon } from "./vmShared";

describe("vmOSIcon", () => {
  it("maps OS ids to logos and falls back sensibly", () => {
    expect(vmOSIcon("ubuntu")).toBe("simple-icons:ubuntu");
    expect(vmOSIcon("homeassistant")).toBe("simple-icons:homeassistant");
    expect(vmOSIcon("win")).toBe("mdi:microsoft-windows");
    expect(vmOSIcon("silverblue")).toBe("simple-icons:linux");
    expect(vmOSIcon(undefined)).toBe("mdi:monitor");
  });
});

describe("bundled VM OS icons", () => {
  it("registers every VM logo without needing an API lookup", () => {
    for (const os of [
      undefined,
      "win11",
      "archlinux",
      "centos-stream",
      "freebsd",
      "homeassistant",
      "ubuntu",
      "silverblue",
    ]) {
      const icon = vmOSIcon(os);
      expect(getIcon(icon)?.body, icon).toBeTruthy();
    }
  });
});
