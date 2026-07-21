import { describe, expect, it } from "vitest";

import {
  getIntegrityCheckOption,
  INDEXER_INTEGRITY_CHECK_OPTIONS,
} from "@/components/navbar/indexerIntegrity";

describe("indexer integrity check options", () => {
  it("exposes every indexer mode", () => {
    expect(INDEXER_INTEGRITY_CHECK_OPTIONS.map(({ value }) => value)).toEqual([
      "full",
      "quick",
      "off",
    ]);
  });

  it("describes the selected mode", () => {
    expect(getIntegrityCheckOption("quick")).toMatchObject({
      label: "Quick",
      value: "quick",
    });
  });

  it("falls back to the indexer default", () => {
    expect(getIntegrityCheckOption(undefined).value).toBe("full");
    expect(getIntegrityCheckOption("unsupported").value).toBe("full");
  });
});
