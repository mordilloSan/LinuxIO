import {
  QueryClient,
  QueryClientProvider,
  useQueries,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, expectTypeOf, it, vi } from "vitest";

import {
  type ActionConfig,
  linuxio,
  type ComposeTaskResult,
  type ContainerIDRequest,
  type DockerComposeRequest,
  useCallMutation,
} from "@/api";
import { LinuxIOError } from "@/api/errors";
import type { TaskSnapshot } from "@/api/generated/linuxio-types";
import { getRouteMode } from "@/api/generated/route-metadata";
import * as core from "@/api/linuxio-core";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

describe("generated Call and Mutation definitions", () => {
  it("exposes only the capabilities owned by each route kind", () => {
    const jobStart: Awaited<ReturnType<typeof linuxio.docker.compose.start>> =
      taskSnapshot();
    const callHasLegacyAction: "useAction" extends keyof typeof linuxio.tasks.get
      ? true
      : false = false;
    const callHasQueryOptions: "queryOptions" extends keyof typeof linuxio.tasks.get
      ? true
      : false = false;
    const callHasMutation: "useMutation" extends keyof typeof linuxio.tasks.get
      ? true
      : false = false;
    const mutationHasTaskAction: "useTaskStreamAction" extends keyof typeof linuxio.docker.compose
      ? true
      : false = false;
    const callActionConfig: ActionConfig<ContainerIDRequest, void> = {
      success: (result, variables) => {
        expectTypeOf(result).toEqualTypeOf<void>();
        expectTypeOf(variables).toEqualTypeOf<ContainerIDRequest>();
      },
    };
    const mutationConfig: NonNullable<
      Parameters<typeof linuxio.docker.compose.useMutation>[0]
    > = {
      success: (result, variables) => {
        expectTypeOf(result).toEqualTypeOf<ComposeTaskResult>();
        expectTypeOf(variables).toEqualTypeOf<DockerComposeRequest>();
      },
    };

    expect({
      callHasLegacyAction,
      callHasQueryOptions,
      callHasMutation,
      mutationHasTaskAction,
    }).toEqual({
      callHasLegacyAction: false,
      callHasQueryOptions: false,
      callHasMutation: false,
      mutationHasTaskAction: false,
    });
    expect(linuxio.tasks.get).toHaveProperty("route", "tasks.get");
    expect(linuxio.tasks.get).not.toHaveProperty("queryOptions");
    expect(linuxio.system.get_cpu_info).toHaveProperty("queryFn");
    expect(linuxio.system.get_cpu_info).toHaveProperty("queryKey");
    expect(linuxio.docker.compose).toHaveProperty("useMutation");
    expect(linuxio.docker.compose).toHaveProperty("start");
    expect(linuxio.docker.compose).not.toHaveProperty("queryOptions");
    expect(callActionConfig.success).toBeTypeOf("function");
    expect(mutationConfig.success).toBeTypeOf("function");
    expectTypeOf(jobStart).toEqualTypeOf<TaskSnapshot>();
  });

  it("builds canonical keys for no-request and request Calls", () => {
    expect(linuxio.system.get_host_info.queryKey).toEqual([
      "linuxio",
      "system",
      "get_host_info",
    ]);
    expect(linuxio.tasks.get({ taskId: "task-1" }).queryKey).toEqual([
      "linuxio",
      "tasks",
      "get",
      { taskId: "task-1" },
    ]);
  });

  it("forwards AbortSignal and applies route-owned retry safety", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue({ ok: true });
    const controller = new AbortController();
    const read = linuxio.tasks.get({ taskId: "task-1" });
    if (typeof read.queryFn !== "function") throw new Error("missing queryFn");

    await expect(
      read.queryFn({ signal: controller.signal } as never),
    ).resolves.toEqual({ ok: true });
    expect(request).toHaveBeenLastCalledWith(
      "tasks",
      "get",
      { taskId: "task-1" },
      { retryPolicy: "connection_loss", signal: controller.signal },
    );

    const write = linuxio.docker.check_container_update({ containerId: "abc" });
    if (typeof write.queryFn !== "function") throw new Error("missing queryFn");
    await write.queryFn({ signal: controller.signal } as never);
    expect(request).toHaveBeenLastCalledWith(
      "docker",
      "check_container_update",
      { containerId: "abc" },
      { retryPolicy: "none", signal: controller.signal },
    );
  });

  it("keeps task primitives such as cancel Calls", () => {
    expect(getRouteMode("tasks.cancel")).toBe("call");
  });
});

function taskSnapshot(overrides: Partial<TaskSnapshot> = {}): TaskSnapshot {
  return {
    created_at: "2026-01-01T00:00:00.000Z",
    id: "task-1",
    state: "completed",
    type: "test",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function renderQueryHook<T>(useHook: () => T) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  const invalidateSpy = vi
    .spyOn(queryClient, "invalidateQueries")
    .mockResolvedValue();
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
  return { invalidateSpy, ...renderHook(useHook, { wrapper }) };
}

describe("Call queries and mutations", () => {
  it("composes one descriptor per request with useQueries", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockImplementation((_handler, _command, wireRequest) =>
        Promise.resolve(wireRequest),
      );

    const { result } = renderQueryHook(() =>
      useQueries({
        queries: ["a", "b"].map((taskId) => ({
          ...linuxio.tasks.get({ taskId }),
          staleTime: Infinity,
        })),
      }),
    );

    await waitFor(() =>
      expect(result.current.every((query) => query.isSuccess)).toBe(true),
    );
    expect(result.current.map((query) => query.data)).toEqual([
      { taskId: "a" },
      { taskId: "b" },
    ]);
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("applies manifest invalidation to a mutation", async () => {
    vi.spyOn(core, "mutationRequest").mockResolvedValue(undefined);
    const manifestKeys =
      OPERATION_QUERY_INVALIDATIONS["docker.start_container"];
    const { result, invalidateSpy } = renderQueryHook(() =>
      linuxio.docker.start_container.useMutation(),
    );

    act(() => result.current.mutate({ containerId: "abc" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    for (const queryKey of manifestKeys) {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey });
    }
    expect(invalidateSpy).toHaveBeenCalledTimes(manifestKeys.length);
  });

  it("runs Call feedback and escape-hatch callbacks in order", async () => {
    vi.spyOn(core, "request").mockResolvedValue({ valid: true });
    const success = vi.fn();
    const onSuccess = vi.fn();
    const { result } = renderQueryHook(() =>
      useCallMutation(linuxio.docker.validate_compose, {
        invalidates: [],
        success,
        options: { onSuccess },
      }),
    );

    act(() => result.current.mutate({ content: "services: {}" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(success).toHaveBeenCalledWith(
      { valid: true },
      { content: "services: {}" },
    );
    expect(success.mock.invocationCallOrder[0]).toBeLessThan(
      onSuccess.mock.invocationCallOrder[0],
    );
  });

  it("prefers a server mutation error over the configured fallback", async () => {
    const { toast } = await import("sonner");
    vi.spyOn(core, "request").mockRejectedValue(new LinuxIOError("bad yaml"));
    const { result } = renderQueryHook(() =>
      useCallMutation(linuxio.docker.validate_compose, {
        error: "Validation failed",
      }),
    );

    act(() => result.current.mutate({ content: "nope" }));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(toast.error).toHaveBeenCalledWith("bad yaml", undefined);
  });
});
