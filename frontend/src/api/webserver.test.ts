import { afterEach, describe, expect, it, vi } from "vitest";

import { login } from "./webserver";

describe("login", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("explains a too_many_requests refusal instead of the generic failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            code: "too_many_requests",
            error: "too many login attempts, try again shortly",
          }),
          { status: 503 },
        ),
      ),
    );

    await expect(login("miguel", "secret")).rejects.toThrow(
      "Too many sign-in attempts at once. Wait a moment and try again.",
    );
  });

  it("falls back to the server message for an unknown code", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ code: "nope", error: "from server" }), {
          status: 500,
        }),
      ),
    );

    await expect(login("miguel", "secret")).rejects.toThrow("from server");
  });
});
