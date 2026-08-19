import { afterEach, describe, expect, it } from "vitest";

import {
  installTabNavigationIntent,
  isTabNavigationActive,
} from "@/utils/tabNavigation";

describe("tab navigation intent", () => {
  let release: (() => void) | undefined;

  afterEach(() => {
    release?.();
    release = undefined;
    document.documentElement.removeAttribute("data-tab-navigation");
  });

  it("enables only for Tab and disables on pointer or window loss", () => {
    release = installTabNavigationIntent();
    const root = document.documentElement;

    for (const key of ["Escape", "Enter", " ", "ArrowDown", "a"]) {
      window.dispatchEvent(new KeyboardEvent("keydown", { key }));
    }
    expect(root).not.toHaveAttribute("data-tab-navigation");
    expect(isTabNavigationActive()).toBe(false);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab" }));
    expect(root).toHaveAttribute("data-tab-navigation", "true");
    expect(isTabNavigationActive()).toBe(true);

    window.dispatchEvent(new Event("pointerdown"));
    expect(root).not.toHaveAttribute("data-tab-navigation");

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab" }));
    window.dispatchEvent(new Event("blur"));
    expect(root).not.toHaveAttribute("data-tab-navigation");
  });

  it("disables when the document becomes hidden", () => {
    release = installTabNavigationIntent();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab" }));
    expect(document.documentElement).toHaveAttribute("data-tab-navigation");

    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
    expect(document.documentElement).not.toHaveAttribute("data-tab-navigation");
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: false,
    });
  });

  it("does not duplicate listeners and tears down after the final release", () => {
    const first = installTabNavigationIntent();
    const second = installTabNavigationIntent();
    release = () => {
      first();
      second();
    };

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab" }));
    expect(document.documentElement).toHaveAttribute("data-tab-navigation");

    first();
    window.dispatchEvent(new Event("pointerdown"));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab" }));
    expect(document.documentElement).toHaveAttribute("data-tab-navigation");

    second();
    release = undefined;
    expect(document.documentElement).not.toHaveAttribute("data-tab-navigation");
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab" }));
    expect(document.documentElement).not.toHaveAttribute("data-tab-navigation");
  });
});
