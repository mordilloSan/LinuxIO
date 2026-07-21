import { afterEach, describe, expect, it, vi } from "vitest";

import { redirectToSignIn } from "@/utils/navigation";

describe("redirectToSignIn", () => {
  const originalLocation = window.location;
  let assign: ReturnType<typeof vi.fn>;

  const setLocation = (pathname: string, search = "", hash = "") => {
    assign = vi.fn();
    Object.defineProperty(window, "location", {
      configurable: true,
      writable: true,
      value: { pathname, search, hash, assign },
    });
  };

  afterEach(() => {
    Object.defineProperty(window, "location", {
      configurable: true,
      writable: true,
      value: originalLocation,
    });
  });

  it("navigates to bare /sign-in when the path is not preserved", () => {
    setLocation("/docker", "?tab=logs", "#section");

    redirectToSignIn();

    expect(assign).toHaveBeenCalledWith("/sign-in");
  });

  it("captures path, search and hash as a redirect param when preserving", () => {
    setLocation("/docker", "?tab=logs", "#section");

    redirectToSignIn(true);

    expect(assign).toHaveBeenCalledWith(
      `/sign-in?redirect=${encodeURIComponent("/docker?tab=logs#section")}`,
    );
  });

  it("does not self-redirect when already on /sign-in", () => {
    setLocation("/sign-in", "?redirect=%2Fdocker");

    redirectToSignIn(true);

    expect(assign).toHaveBeenCalledWith("/sign-in");
  });
});
