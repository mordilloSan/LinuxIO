import { describe, expect, it } from "vitest";

import { goDurationToMs } from "./durations";

describe("goDurationToMs", () => {
  it("converts Go duration strings to milliseconds", () => {
    expect(goDurationToMs("5m")).toBe(300_000);
    expect(goDurationToMs("1m30s")).toBe(90_000);
    expect(goDurationToMs(" 1h0m0s ")).toBe(3_600_000);
    expect(goDurationToMs("0")).toBe(0);
  });

  it("returns null for anything that is not a Go duration", () => {
    expect(goDurationToMs("")).toBeNull();
    expect(goDurationToMs("5")).toBeNull();
    expect(goDurationToMs("five minutes")).toBeNull();
    expect(goDurationToMs("-5m")).toBeNull();
  });
});
