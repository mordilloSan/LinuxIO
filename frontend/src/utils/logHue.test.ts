import { describe, expect, it } from "vitest";

import { logSourceHue } from "./logHue";

describe("logSourceHue", () => {
  it("is stable per name and within the hue circle", () => {
    expect(logSourceHue("web")).toBe(logSourceHue("web"));
    expect(logSourceHue("web")).not.toBe(logSourceHue("db"));
    for (const name of ["web", "db", "cache", "a", ""]) {
      const hue = logSourceHue(name);
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
  });
});
