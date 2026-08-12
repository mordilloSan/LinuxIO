import { afterEach, describe, expect, it, vi } from "vitest";

import { call, getRetryPolicy } from "./calls";
import * as core from "./linuxio-core";

describe("Call retry policy", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("uses only generated route policy", () => {
    expect(getRetryPolicy("system.get_cpu_info")).toBe("connection_loss");
    expect(getRetryPolicy("docker.stop_container")).toBe("none");
    expect(getRetryPolicy("example.get_state")).toBe("none");
  });

  it("applies generated policy to imperative Calls", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue({ cores: 4 });
    const controller = new AbortController();

    await call("system.get_cpu_info", undefined, {
      signal: controller.signal,
      timeout: 5_000,
    });

    expect(request).toHaveBeenCalledWith(
      "system",
      "get_cpu_info",
      {},
      {
        retryPolicy: "connection_loss",
        signal: controller.signal,
        timeout: 5_000,
      },
    );
  });
});
