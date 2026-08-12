import { useQueryClient } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError } from "@/api";

const toastMocks = vi.hoisted(() => ({ error: vi.fn() }));

vi.mock("sonner", () => ({ toast: toastMocks }));

import AppQueryClientProvider from "./query-client";
import { createQueryClient, getAppQueryClient } from "./query-client-core";

describe("AppQueryClientProvider", () => {
  beforeEach(() => {
    toastMocks.error.mockReset();
  });

  it("provides the stable browser client returned to router infrastructure", async () => {
    let observedClient: ReturnType<typeof getAppQueryClient> | undefined;

    function Observer() {
      const queryClient = useQueryClient();
      useEffect(() => {
        observedClient = queryClient;
      }, [queryClient]);
      return null;
    }

    render(
      <AppQueryClientProvider>
        <Observer />
      </AppQueryClientProvider>,
    );

    await Promise.resolve();
    expect(observedClient).toBe(getAppQueryClient());
    expect(getAppQueryClient()).toBe(observedClient);
  });

  it("keeps explicitly created clients isolated for tests and server renders", () => {
    expect(createQueryClient()).not.toBe(createQueryClient());
  });

  it.each(["connection_unavailable", "outcome_unknown"] as const)(
    "does not repeat the transport's %s retry at Query level",
    async (code) => {
      const queryFn = vi
        .fn()
        .mockRejectedValue(new LinuxIOError("offline", code));

      await expect(
        createQueryClient().fetchQuery({
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
      createQueryClient().fetchQuery({
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
      queryClient.fetchQuery({
        meta: { routeInitialLoad: true, silent: true },
        networkMode: "always",
        queryFn: () => Promise.reject(new Error("route owns this error")),
        queryKey: ["silent-route-error"],
        retry: false,
      }),
    ).rejects.toThrow("route owns this error");
    expect(toastMocks.error).not.toHaveBeenCalled();

    await expect(
      queryClient.fetchQuery({
        networkMode: "always",
        queryFn: () => Promise.reject(new Error("background failure")),
        queryKey: ["background-error"],
        retry: false,
      }),
    ).rejects.toThrow("background failure");
    expect(toastMocks.error).toHaveBeenCalledWith("background failure");
  });
});
