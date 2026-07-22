import { afterEach, describe, expect, it } from "vitest";

import {
  clearSigninNotice,
  consumeSigninNotice,
  readSigninNotice,
  setSigninNotice,
} from "@/utils/signinNotice";

describe("signinNotice", () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it("round-trips a notice through session storage", () => {
    setSigninNotice("expired");

    expect(consumeSigninNotice()).toBe("expired");
  });

  it("is one-shot: a second read returns null", () => {
    setSigninNotice("expired");

    expect(consumeSigninNotice()).toBe("expired");
    expect(consumeSigninNotice()).toBeNull();
  });

  it("returns null when no notice is pending", () => {
    expect(consumeSigninNotice()).toBeNull();
  });

  it("rejects and clears unrecognized stored values", () => {
    sessionStorage.setItem("signin_notice", "unknown");

    expect(consumeSigninNotice()).toBeNull();
    expect(sessionStorage.getItem("signin_notice")).toBeNull();
  });

  it("supports a non-mutating read followed by an explicit clear", () => {
    setSigninNotice("expired");

    expect(readSigninNotice()).toBe("expired");
    expect(readSigninNotice()).toBe("expired");

    clearSigninNotice();
    expect(readSigninNotice()).toBeNull();
  });
});
