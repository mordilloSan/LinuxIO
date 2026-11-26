import { describe, expect, it } from "vitest";

import { DEFAULT_POOL_TAB, validatePoolTab } from "./poolTabs";

describe("validatePoolTab", () => {
  it("keeps a known tab and drops anything else", () => {
    expect(validatePoolTab({ tab: "snapshots" })).toEqual({ tab: "snapshots" });
    expect(validatePoolTab({ tab: "nope" })).toEqual({});
    expect(validatePoolTab({})).toEqual({});
    expect(DEFAULT_POOL_TAB).toBe("overview");
  });
});
