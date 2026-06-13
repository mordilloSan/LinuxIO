import { describe, expect, it } from "vitest";

import { LinuxIOError } from "@/api";
import { getMutationErrorMessage } from "@/utils/mutations";

describe("getMutationErrorMessage", () => {
  it("uses structured LinuxIOError messages", () => {
    expect(
      getMutationErrorMessage(new LinuxIOError("quota exceeded", 400), "bad"),
    ).toBe("quota exceeded");
  });

  it("uses regular Error messages", () => {
    expect(getMutationErrorMessage(new Error("failed"), "fallback")).toBe(
      "failed",
    );
  });

  it("falls back for empty or unknown errors", () => {
    expect(getMutationErrorMessage(new Error(""), "fallback")).toBe("fallback");
    expect(getMutationErrorMessage("bad", "fallback")).toBe("fallback");
    expect(getMutationErrorMessage(null, "fallback")).toBe("fallback");
  });
});
