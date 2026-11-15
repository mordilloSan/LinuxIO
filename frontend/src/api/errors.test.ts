import { describe, expect, it } from "vitest";

import {
  LinuxIOError,
  classifyRequestOutcome,
  getMutationErrorMessage,
} from "./errors";

describe("classifyRequestOutcome", () => {
  it.each([
    ["connection_unavailable", "not sent", "not_sent"],
    [400, "bad request", "failed"],
    [403, "forbidden", "failed"],
    [409, "update_pending: LinuxIO update waits for running jobs", "failed"],
    [499, "operation aborted", "failed"],
    // A plain handler error: logind denying a reboot, NetworkManager
    // rejecting a setting.
    [500, "failed to call Reboot: access denied", "failed"],
    [500, "job failed", "failed"],
    [409, "job was interrupted", "unknown"],
    [503, "job host is stopping", "unknown"],
    [503, "job host unavailable", "unknown"],
    [504, "gateway timeout", "unknown"],
    [302, "moved", "unknown"],
    ["outcome_unknown", "lost", "unknown"],
    ["timeout", "slow", "unknown"],
    ["connection_closed", "closed", "unknown"],
    ["some_string_code", "odd", "unknown"],
    [undefined, "no code", "unknown"],
  ] as const)("classifies %s %s as %s", (code, message, outcome) => {
    expect(classifyRequestOutcome(new LinuxIOError(message, code))).toBe(
      outcome,
    );
  });

  it("calls an error that is not a LinuxIOError unknown", () => {
    expect(classifyRequestOutcome(new Error("plain"))).toBe("unknown");
    expect(classifyRequestOutcome("bad")).toBe("unknown");
  });
});

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
