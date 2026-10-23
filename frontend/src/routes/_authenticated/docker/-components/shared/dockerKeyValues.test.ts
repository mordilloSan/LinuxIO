import { describe, expect, it } from "vitest";

import { parseDockerKeyValueLines } from "./dockerKeyValues";

describe("parseDockerKeyValueLines", () => {
  it("parses non-empty key=value lines and reports the failing line", () => {
    expect(parseDockerKeyValueLines(" purpose = cache\n\nempty= ")).toEqual({
      error: undefined,
      values: { purpose: "cache", empty: "" },
    });
    expect(parseDockerKeyValueLines("valid=yes\ninvalid")).toEqual({
      error: "Line 2 must use key=value.",
      values: {},
    });
  });
});
