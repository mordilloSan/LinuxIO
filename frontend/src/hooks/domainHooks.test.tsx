import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useDockerIcon } from "@/hooks/useDockerIcon";
import { createTestQueryClient, renderHook } from "@/test/render";

const queryClient = createTestQueryClient();
const queryWrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

const apiMocks = vi.hoisted(() => ({
  getIconUriQueryOptions: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        get_icon_uri: Object.assign(apiMocks.getIconUriQueryOptions, {
          route: actual.linuxio.docker.get_icon_uri.route,
        }),
      },
    },
  };
});

describe("useDockerIcon", () => {
  beforeEach(() => {
    queryClient.clear();
    apiMocks.getIconUriQueryOptions.mockImplementation(
      (request: { identifier: string }) => ({
        queryKey: ["test", "docker-icon", request.identifier],
        queryFn: () =>
          Promise.resolve({ uri: "data:image/svg+xml;base64,abc" }),
        initialData: { uri: "data:image/svg+xml;base64,abc" },
      }),
    );
  });

  it("enables icon lookup only when an identifier is present and enabled", () => {
    const { result } = renderHook(() => useDockerIcon("si:nginx"), {
      wrapper: queryWrapper,
    });

    expect(result.current.iconUri).toBe("data:image/svg+xml;base64,abc");
    expect(apiMocks.getIconUriQueryOptions).toHaveBeenCalledWith({
      identifier: "si:nginx",
    });
  });

  it("returns null and disables the query for missing or disabled identifiers", () => {
    const missing = renderHook(() => useDockerIcon(undefined), {
      wrapper: queryWrapper,
    });
    const disabled = renderHook(() => useDockerIcon("si:nginx", false), {
      wrapper: queryWrapper,
    });

    expect(missing.result.current.iconUri).toBeNull();
    expect(disabled.result.current.iconUri).toBe(
      "data:image/svg+xml;base64,abc",
    );
    expect(apiMocks.getIconUriQueryOptions.mock.calls[0]).toEqual([
      { identifier: "" },
    ]);
    expect(apiMocks.getIconUriQueryOptions.mock.calls[1]).toEqual([
      { identifier: "si:nginx" },
    ]);
  });
});
