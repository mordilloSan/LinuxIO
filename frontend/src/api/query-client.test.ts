import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "./errors";
import { appQueryClient, createQueryClient } from "./query-client";

const toastMocks = vi.hoisted(() => ({ error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toastMocks }));

describe("query client", () => {
  beforeEach(() => {
    toastMocks.error.mockReset();
  });

  it("keeps explicitly created clients isolated from the app client", () => {
    expect(createQueryClient()).not.toBe(createQueryClient());
    expect(createQueryClient()).not.toBe(appQueryClient);
  });

  it.each(["connection_unavailable", "outcome_unknown"] as const)(
    "does not repeat the transport's %s retry at Query level",
    async (code) => {
      const queryFn = vi
        .fn()
        .mockRejectedValue(new LinuxIOError("offline", code));

      await expect(
        createQueryClient().query({
          networkMode: "always",
          queryFn,
          queryKey: ["connection-loss", code],
          retryDelay: 0,
        }),
      ).rejects.toMatchObject({ code });

      expect(queryFn).toHaveBeenCalledTimes(1);
    },
  );

  it("retains one Query retry for errors without transport recovery", async () => {
    const queryFn = vi.fn().mockRejectedValue(new Error("temporary failure"));

    await expect(
      createQueryClient().query({
        networkMode: "always",
        queryFn,
        queryKey: ["generic-error"],
        retryDelay: 0,
      }),
    ).rejects.toThrow("temporary failure");

    expect(queryFn).toHaveBeenCalledTimes(2);
  });

  it("leaves silent route failures to the route or widget boundary", async () => {
    const queryClient = createQueryClient();

    await expect(
      queryClient.query({
        meta: { routeInitialLoad: true, silent: true },
        networkMode: "always",
        queryFn: () => Promise.reject(new Error("route owns this error")),
        queryKey: ["silent-route-error"],
        retry: false,
      }),
    ).rejects.toThrow("route owns this error");
    expect(toastMocks.error).not.toHaveBeenCalled();

    await expect(
      queryClient.query({
        networkMode: "always",
        queryFn: () => Promise.reject(new Error("background failure")),
        queryKey: ["background-error"],
        retry: false,
      }),
    ).rejects.toThrow("background failure");
    expect(toastMocks.error).toHaveBeenCalledWith("background failure");
  });
});
