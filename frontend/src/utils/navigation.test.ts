import { afterEach, describe, expect, it } from "vitest";

import { buildSignInUrl } from "@/utils/navigation";

describe("buildSignInUrl", () => {
  afterEach(() => {
    window.history.pushState({}, "", "/");
  });

  it("returns bare /sign-in when the path is not preserved", () => {
    window.history.pushState({}, "", "/docker?tab=logs#section");

    expect(buildSignInUrl()).toBe("/sign-in");
  });

  it("captures path, search and hash as a redirect param when preserving", () => {
    window.history.pushState({}, "", "/docker?tab=logs#section");

    expect(buildSignInUrl(true)).toBe(
      `/sign-in?redirect=${encodeURIComponent("/docker?tab=logs#section")}`,
    );
  });

  it("does not self-redirect when already on /sign-in", () => {
    window.history.pushState({}, "", "/sign-in?redirect=%2Fdocker");

    expect(buildSignInUrl(true)).toBe("/sign-in");
  });
});
