import { afterEach, describe, expect, it } from "vitest";

import {
  clearSigninNotice,
  readSigninNotice,
  setSigninNotice,
} from "@/utils/signinNotice";

describe("signinNotice", () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it("round-trips a notice through session storage", () => {
    setSigninNotice("expired");

    expect(readSigninNotice()).toBe("expired");
  });

  it("returns null when no notice is pending", () => {
    expect(readSigninNotice()).toBeNull();
  });

  it("rejects unrecognized stored values and clears them on request", () => {
    sessionStorage.setItem("signin_notice", "unknown");

    expect(readSigninNotice()).toBeNull();
    clearSigninNotice();
    expect(sessionStorage.getItem("signin_notice")).toBeNull();
  });

  it("reads without mutating until an explicit clear", () => {
    setSigninNotice("expired");

    expect(readSigninNotice()).toBe("expired");
    expect(readSigninNotice()).toBe("expired");

    clearSigninNotice();
    expect(readSigninNotice()).toBeNull();
  });
});
