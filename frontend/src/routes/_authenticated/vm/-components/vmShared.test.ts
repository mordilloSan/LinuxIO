import { getIcon } from "@iconify/react";
import { describe, expect, it } from "vitest";

import "@/icons/icons";
import "@/icons/shell";

import {
  VM_OS_OPTIONS,
  isValidVMName,
  vmOSColor,
  vmOSIcon,
  vmOSLabel,
} from "./vmShared";

describe("isValidVMName", () => {
  it("applies the bridge rule", () => {
    expect(isValidVMName("web-1_a.b")).toBe(true);
    expect(isValidVMName("1")).toBe(true);
    expect(isValidVMName("a".repeat(63))).toBe(true);
    expect(isValidVMName("a".repeat(64))).toBe(false);
    expect(isValidVMName("")).toBe(false);
    expect(isValidVMName(".hidden")).toBe(false);
    expect(isValidVMName("-dash")).toBe(false);
    expect(isValidVMName("_under")).toBe(false);
    expect(isValidVMName("a..b")).toBe(false);
    expect(isValidVMName("a b")).toBe(false);
  });
});

describe("vmOSIcon", () => {
  it("maps OS ids to logos and falls back sensibly", () => {
    expect(vmOSIcon("ubuntu")).toBe("mdi:ubuntu");
    expect(vmOSIcon("homeassistant")).toBe("simple-icons:homeassistant");
    expect(vmOSIcon("win")).toBe("mdi:microsoft-windows");
    expect(vmOSIcon("silverblue")).toBe("simple-icons:linux");
    expect(vmOSIcon(undefined)).toBe("mdi:monitor");
  });
});

describe("vmOSColor", () => {
  it("gives a logo its brand colour and a dark or unknown one the fallback", () => {
    expect(vmOSColor("ubuntu", "fallback")).toBe("#E95420");
    expect(vmOSColor("homeassistant", "fallback")).toBe("#18BCF2");
    expect(vmOSColor("win", "fallback")).toBe("#0078D4");
    expect(vmOSColor("almalinux", "fallback")).toBe("fallback");
    expect(vmOSColor(undefined, "fallback")).toBe("fallback");
  });
});

describe("vmOSLabel", () => {
  it("names known ids and keeps unknown ones readable", () => {
    expect(vmOSLabel("homeassistant")).toBe("Home Assistant OS");
    expect(vmOSLabel("silverblue")).toBe("silverblue");
    expect(vmOSLabel(undefined)).toBe("Unknown");
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
      ...VM_OS_OPTIONS.map((option) => option.id),
    ]) {
      const icon = vmOSIcon(os);
      expect(getIcon(icon)?.body, icon).toBeTruthy();
    }
  });
});
