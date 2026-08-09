import {
  QueryClient,
  QueryClientProvider,
  useQueries,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import {
  type ActionConfig,
  linuxio,
  type ComposeTaskMessage,
  type ComposeTaskResult,
  type ContainerIDRequest,
  type DockerComposeRequest,
  useCallMutation,
} from "@/api";
import type { TaskSnapshot } from "@/api/generated/linuxio-types";
import { getRouteMode } from "@/api/generated/route-metadata";
import { openTaskWatchStream } from "@/api/linuxio";
import * as core from "@/api/linuxio-core";
import { LinuxIOError } from "@/api/linuxio-core";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import type { Stream } from "@/api/StreamMultiplexer";
import { createTaskEndpoint } from "@/api/task-react-query";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

vi.mock("@/api/linuxio", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/linuxio")>();
  return {
    ...actual,
    openTaskWatchStream: vi.fn(),
  };
});

describe("generated Call and Task definitions", () => {
  it("exposes only the capabilities owned by each route kind", () => {
    const callHasLegacyAction: "useAction" extends keyof typeof linuxio.tasks.get
      ? true
      : false = false;
    const callHasQueryOptions: "queryOptions" extends keyof typeof linuxio.tasks.get
      ? true
      : false = false;
    const callHasTaskAction: "useTaskAction" extends keyof typeof linuxio.tasks.get
      ? true
      : false = false;
    const taskHasTaskAction: "useTaskAction" extends keyof typeof linuxio.docker.compose
      ? true
      : false = true;
    const taskHasTaskStreamAction: "useTaskStreamAction" extends keyof typeof linuxio.docker.compose
      ? true
      : false = true;
    const callActionConfig: ActionConfig<ContainerIDRequest, void> = {
      success: (result, variables) => {
        const typedResult: void = result;
        const typedVariables: ContainerIDRequest = variables;
        void typedResult;
        void typedVariables;
      },
    };
    const taskActionConfig: NonNullable<
      Parameters<typeof linuxio.docker.compose.useTaskAction>[0]
    > = {
      success: (result, variables) => {
        const typedResult: ComposeTaskResult = result;
        const typedVariables: DockerComposeRequest = variables;
        void typedResult;
        void typedVariables;
      },
    };

    expect({
      callHasLegacyAction,
      callHasQueryOptions,
      callHasTaskAction,
      taskHasTaskAction,
      taskHasTaskStreamAction,
    }).toEqual({
      callHasLegacyAction: false,
      callHasQueryOptions: false,
      callHasTaskAction: false,
      taskHasTaskAction: true,
      taskHasTaskStreamAction: true,
    });
    expect(linuxio.tasks.get).toHaveProperty("route", "tasks.get");
    expect(linuxio.tasks.get).not.toHaveProperty("queryOptions");
    expect(linuxio.system.get_cpu_info).toHaveProperty("queryFn");
    expect(linuxio.system.get_cpu_info).toHaveProperty("queryKey");
    expect(linuxio.docker.compose).toHaveProperty("useTaskAction");
    expect(linuxio.docker.compose).not.toHaveProperty("queryOptions");
    expect(callActionConfig.success).toBeTypeOf("function");
    expect(taskActionConfig.success).toBeTypeOf("function");
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
      { retryPolicy: "none", signal: controller.signal },
    );

    const write = linuxio.docker.stop_container({ containerId: "abc" });
    if (typeof write.queryFn !== "function") throw new Error("missing queryFn");
    await write.queryFn({ signal: controller.signal } as never);
    expect(request).toHaveBeenLastCalledWith(
      "docker",
      "stop_container",
      { containerId: "abc" },
      { retryPolicy: "none", signal: controller.signal },
    );
  });

  it("keeps task primitives such as cancel out of Task route mode", () => {
    expect(getRouteMode("tasks.cancel")).toBe("call");
    expect(() =>
      createTaskEndpoint("tasks", "cancel", {
        kind: "field",
        field: "taskId",
      }).useTaskAction(),
    ).toThrow(/not mutation\/task/);
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

function createStream(): Stream {
  const stream: Stream = {
    abort: vi.fn(() => stream.onClose?.()),
    close: vi.fn(() => stream.onClose?.()),
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "tasks.watch",
    write: vi.fn(),
  };
  return stream;
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

  it("applies manifest invalidation to a Call mutation", async () => {
    vi.spyOn(core, "request").mockResolvedValue(undefined);
    const manifestKeys =
      OPERATION_QUERY_INVALIDATIONS["docker.start_container"];
    const { result, invalidateSpy } = renderQueryHook(() =>
      useCallMutation(linuxio.docker.start_container),
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

describe("Task React Query integration", () => {
  it("rejects Call and Duplex routes", () => {
    expect(() =>
      createTaskEndpoint("system", "get_cpu_info", {
        kind: "none",
      }).useTaskAction(),
    ).toThrow(/not mutation\/task/);
    expect(() =>
      createTaskEndpoint("container", "open", {
        kind: "object",
      }).useTaskAction(),
    ).toThrow(/not mutation\/task/);
  });

  it("unwraps a terminal Task result and invalidates its keys", async () => {
    vi.spyOn(core, "request").mockResolvedValue(
      taskSnapshot({ result: { updated: true } }),
    );
    const endpoint = createTaskEndpoint<{ updated: boolean }>(
      "virt",
      "create",
      { kind: "object" },
    );
    const { result, invalidateSpy } = renderQueryHook(() =>
      endpoint.useTaskAction({ invalidates: [["linuxio", "virt", "list"]] }),
    );

    act(() => result.current.mutate({ name: "vm" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({ updated: true });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["linuxio", "virt", "list"],
    });
  });

  it("watches Task progress and resolves its terminal result", async () => {
    vi.spyOn(core, "request").mockResolvedValue(
      taskSnapshot({ state: "running" }),
    );
    const stream = createStream();
    vi.mocked(openTaskWatchStream).mockReturnValue(stream);
    const onProgress = vi.fn();
    const request = { action: "up", projectName: "web" };
    const { result } = renderQueryHook(() =>
      linuxio.docker.compose.useTaskStreamAction({
        invalidates: [],
        onProgress: (progress) => {
          const message: ComposeTaskMessage = progress;
          onProgress(message);
        },
      }),
    );

    act(() => result.current.mutate(request));
    await waitFor(() =>
      expect(openTaskWatchStream).toHaveBeenCalledWith("task-1"),
    );
    const progress = { message: "Pulling image", type: "progress" } as const;
    act(() => stream.onProgress?.(progress as never));
    expect(onProgress).toHaveBeenCalledWith(progress);

    const complete = { message: "done", type: "complete" } as const;
    act(() => stream.onResult?.({ data: complete, status: "ok" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const terminalResult: ComposeTaskResult | undefined = result.current.data;
    expect(terminalResult).toEqual(complete);
  });
});
