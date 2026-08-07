import {
  QueryClient,
  QueryClientProvider,
  useQueries,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { linuxio, type ComposeJobMessage, type ComposeJobResult } from "@/api";
import type { JobSnapshot } from "@/api/generated/linuxio-types";
import { getRouteMode } from "@/api/generated/route-metadata";
import { openJobAttachStream } from "@/api/linuxio";
import * as core from "@/api/linuxio-core";
import { LinuxIOError } from "@/api/linuxio-core";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import { createEndpoint } from "@/api/react-query";
import type { Stream } from "@/api/StreamMultiplexer";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/api/linuxio", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/linuxio")>();
  return {
    ...actual,
    openJobAttachStream: vi.fn(),
  };
});

describe("createEndpoint", () => {
  it("exposes only the hooks allowed by each generated route mode", () => {
    const queryHasAction: "useAction" extends keyof typeof linuxio.jobs.get
      ? true
      : false = true;
    const queryHasJobAction: "useJobAction" extends keyof typeof linuxio.jobs.get
      ? true
      : false = false;
    const queryHasJobStreamAction: "useJobStreamAction" extends keyof typeof linuxio.jobs.get
      ? true
      : false = false;
    const jobHasAction: "useAction" extends keyof typeof linuxio.docker.compose
      ? true
      : false = false;
    const jobHasQueryOptions: "queryOptions" extends keyof typeof linuxio.docker.compose
      ? true
      : false = false;
    const jobHasJobAction: "useJobAction" extends keyof typeof linuxio.docker.compose
      ? true
      : false = true;
    const jobHasJobStreamAction: "useJobStreamAction" extends keyof typeof linuxio.docker.compose
      ? true
      : false = true;

    expect({
      jobHasAction,
      jobHasJobAction,
      jobHasJobStreamAction,
      jobHasQueryOptions,
      queryHasAction,
      queryHasJobAction,
      queryHasJobStreamAction,
    }).toEqual({
      jobHasAction: false,
      jobHasJobAction: true,
      jobHasJobStreamAction: true,
      jobHasQueryOptions: false,
      queryHasAction: true,
      queryHasJobAction: false,
      queryHasJobStreamAction: false,
    });
  });

  it("builds deterministic query keys for no-request, field, and object shapes", () => {
    expect(
      createEndpoint("system", "get_cpu_info", { kind: "none" }).queryKey(),
    ).toEqual(["linuxio", "system", "get_cpu_info"]);

    expect(
      createEndpoint("jobs", "get", {
        kind: "field",
        field: "jobId",
      }).queryKey("job-1"),
    ).toEqual(["linuxio", "jobs", "get", { jobId: "job-1" }]);

    expect(
      createEndpoint("config", "set", { kind: "object" }).queryKey({
        theme: "dark",
      }),
    ).toEqual(["linuxio", "config", "set", { theme: "dark" }]);
  });

  it("shapes direct requests and chooses retry policy", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue("ok");

    await expect(
      createEndpoint<string>("system", "get_cpu_info", { kind: "none" })(),
    ).resolves.toBe("ok");
    expect(request).toHaveBeenLastCalledWith(
      "system",
      "get_cpu_info",
      {},
      { retryPolicy: "connection_closed" },
    );

    await createEndpoint("docker", "start_container", {
      kind: "field",
      field: "containerId",
    })("abc");
    expect(request).toHaveBeenLastCalledWith(
      "docker",
      "start_container",
      { containerId: "abc" },
      { retryPolicy: "none" },
    );
  });

  it("builds query options and rejects invalid route modes", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue({ ok: true });
    const endpoint = createEndpoint("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    const options = endpoint.queryOptions("job-1", { staleTime: 123 });
    expect(options.queryKey).toEqual([
      "linuxio",
      "jobs",
      "get",
      { jobId: "job-1" },
    ]);
    expect(options.staleTime).toBe(123);
    if (typeof options.queryFn !== "function") {
      throw new Error("Expected queryFn to be callable");
    }
    const controller = new AbortController();
    await expect(
      options.queryFn({ signal: controller.signal } as never),
    ).resolves.toEqual({ ok: true });
    expect(request).toHaveBeenLastCalledWith(
      "jobs",
      "get",
      { jobId: "job-1" },
      { retryPolicy: "none", signal: controller.signal },
    );

    expect(() =>
      createEndpoint("virt", "create", { kind: "object" }).queryOptions({}),
    ).toThrow(/not query/);

    expect(() =>
      createEndpoint("system", "get_cpu_info", { kind: "none" }).useJobAction(),
    ).toThrow(/not mutation\/job/);
  });

  // jobs.cancel returns the *target* job's snapshot rather than starting one.
  // While it was declared job-mode, `useJobAction` type-checked, then fed that
  // snapshot to waitForJobCompletion, which threw 499 for an already-canceled
  // job or opened a second attach stream for a still-running one. Query mode is
  // what makes `useJobAction` on it fail instead.
  it("keeps jobs.cancel out of job mode so useJobAction cannot await it", () => {
    expect(getRouteMode("jobs.cancel")).toBe("query");

    expect(() =>
      createEndpoint("jobs", "cancel", {
        kind: "field",
        field: "jobId",
      }).useJobAction(),
    ).toThrow(/not mutation\/job/);
  });
});

function jobSnapshot(overrides: Partial<JobSnapshot> = {}): JobSnapshot {
  return {
    created_at: "2026-01-01T00:00:00.000Z",
    id: "job-1",
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
    type: "jobs.attach",
    write: vi.fn(),
  };
  return stream;
}

function renderJobAction<T>(useHook: () => T) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  const invalidateSpy = vi
    .spyOn(queryClient, "invalidateQueries")
    .mockResolvedValue();
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
  return { invalidateSpy, ...renderHook(useHook, { wrapper }) };
}

describe("useJobAction", () => {
  it("rejects query and duplex routes", () => {
    expect(() =>
      createEndpoint("system", "get_cpu_info", { kind: "none" }).useJobAction(),
    ).toThrow(/not mutation\/job/);

    expect(() =>
      createEndpoint("container", "open", { kind: "object" }).useJobAction(),
    ).toThrow(/not mutation\/job/);
  });

  it("unwraps the job result, invalidates keys, and toasts success", async () => {
    const { toast } = await import("sonner");
    vi.spyOn(core, "request").mockResolvedValue(
      jobSnapshot({ result: { updated: true } }),
    );
    const endpoint = createEndpoint<{ updated: boolean }>("virt", "create", {
      kind: "object",
    });

    const { result, invalidateSpy } = renderJobAction(() =>
      endpoint.useJobAction({
        invalidates: [["linuxio", "virt", "list"]],
        success: "VM created",
        toast: { label: "Open VMs", to: "/vm" },
      }),
    );

    act(() => result.current.mutate({ name: "vm" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual({ updated: true });
    expect(core.request).toHaveBeenCalledWith(
      "virt",
      "create",
      { name: "vm" },
      { retryPolicy: "none" },
    );
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["linuxio", "virt", "list"],
    });
    expect(toast.success).toHaveBeenCalledWith("VM created", {
      meta: { label: "Open VMs", to: "/vm" },
    });
  });

  it("passes non-job results through unchanged", async () => {
    vi.spyOn(core, "request").mockResolvedValue({ ok: true });
    const endpoint = createEndpoint<{ ok: boolean }>("virt", "create", {
      kind: "object",
    });

    const { result } = renderJobAction(() => endpoint.useJobAction());

    act(() => result.current.mutate({ name: "vm" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({ ok: true });
  });

  it("applies default invalidation to direct actions", async () => {
    vi.spyOn(core, "request").mockResolvedValue(undefined);
    const endpoint = createEndpoint("docker", "start_container", {
      kind: "field",
      field: "containerId",
    });

    const manifestKeys =
      OPERATION_QUERY_INVALIDATIONS["docker.start_container"];
    expect(manifestKeys.length).toBeGreaterThan(0);

    const { result, invalidateSpy } = renderJobAction(() =>
      endpoint.useAction(),
    );
    act(() => result.current.mutate("abc"));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    for (const queryKey of manifestKeys) {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey });
    }
    expect(invalidateSpy).toHaveBeenCalledTimes(manifestKeys.length);
  });

  it("lets explicit invalidates config override or suppress the manifest", async () => {
    vi.spyOn(core, "request").mockResolvedValue(jobSnapshot());
    const endpoint = createEndpoint("virt", "create", {
      kind: "object",
    });

    const override = renderJobAction(() =>
      endpoint.useJobAction({ invalidates: [["custom", "key"]] }),
    );
    act(() => override.result.current.mutate({ name: "vm" }));
    await waitFor(() => expect(override.result.current.isSuccess).toBe(true));
    expect(override.invalidateSpy).toHaveBeenCalledTimes(1);
    expect(override.invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["custom", "key"],
    });

    const optOut = renderJobAction(() =>
      endpoint.useJobAction({ invalidates: [] }),
    );
    act(() => optOut.result.current.mutate({ name: "vm" }));
    await waitFor(() => expect(optOut.result.current.isSuccess).toBe(true));
    expect(optOut.invalidateSpy).not.toHaveBeenCalled();
  });

  it("supports callback forms and runs escape-hatch handlers last", async () => {
    vi.spyOn(core, "request").mockResolvedValue(
      jobSnapshot({ result: { name: "web" } }),
    );
    const endpoint = createEndpoint<{ name: string }>("virt", "create", {
      kind: "object",
    });

    const success = vi.fn();
    const onSuccess = vi.fn();
    const { result, invalidateSpy } = renderJobAction(() =>
      endpoint.useJobAction({
        invalidates: (jobResult, variables) => [
          ["linuxio", "docker", jobResult.name, variables],
        ],
        success,
        options: { onSuccess },
      }),
    );

    act(() => result.current.mutate({ containerId: "abc" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["linuxio", "docker", "web", { containerId: "abc" }],
    });
    expect(success).toHaveBeenCalledWith(
      { name: "web" },
      { containerId: "abc" },
    );
    expect(onSuccess).toHaveBeenCalledTimes(1);
    expect(success.mock.invocationCallOrder[0]).toBeLessThan(
      onSuccess.mock.invocationCallOrder[0],
    );
  });

  it("prefers the server error message over the config fallback", async () => {
    const { toast } = await import("sonner");
    vi.spyOn(core, "request").mockRejectedValue(new LinuxIOError("exploded"));
    const endpoint = createEndpoint("virt", "create", {
      kind: "object",
    });

    const { result } = renderJobAction(() =>
      endpoint.useJobAction({ error: "Failed to update container" }),
    );

    act(() => result.current.mutate({ containerId: "abc" }));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(toast.error).toHaveBeenCalledWith("exploded", undefined);
  });

  it("routes failed jobs to the error callback with no toast", async () => {
    const { toast } = await import("sonner");
    vi.spyOn(core, "request").mockResolvedValue(
      jobSnapshot({
        state: "failed",
        error: { code: 500, message: "disk full" },
      }),
    );
    const endpoint = createEndpoint("virt", "create", {
      kind: "object",
    });

    const error = vi.fn();
    const { result } = renderJobAction(() => endpoint.useJobAction({ error }));

    act(() => result.current.mutate({ containerId: "abc" }));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(error).toHaveBeenCalledWith(
      expect.objectContaining({ message: "disk full" }),
      { containerId: "abc" },
    );
    expect(toast.error).not.toHaveBeenCalled();
  });
});

describe("useAction", () => {
  it("rejects job and duplex routes", () => {
    expect(() =>
      createEndpoint("virt", "create", {
        kind: "object",
      }).useAction(),
    ).toThrow(/not query/);

    expect(() =>
      createEndpoint("container", "open", { kind: "object" }).useAction(),
    ).toThrow(/not query/);
  });

  it("runs the command with mutation ergonomics and toasts success", async () => {
    const { toast } = await import("sonner");
    const request = vi
      .spyOn(core, "request")
      .mockResolvedValue({ valid: true });
    const endpoint = createEndpoint<{ valid: boolean }>(
      "docker",
      "validate_compose",
      { kind: "field", field: "content" },
    );

    const { result, invalidateSpy } = renderJobAction(() =>
      endpoint.useAction({ success: "Compose file is valid" }),
    );

    act(() => result.current.mutate({ content: "services: {}" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual({ valid: true });
    expect(request).toHaveBeenCalledWith(
      "docker",
      "validate_compose",
      { content: "services: {}" },
      { retryPolicy: "connection_closed" },
    );
    // This command has no operation manifest entry, so nothing invalidates.
    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(toast.success).toHaveBeenCalledWith(
      "Compose file is valid",
      undefined,
    );
  });

  it("prefers the server error message over the config fallback", async () => {
    const { toast } = await import("sonner");
    vi.spyOn(core, "request").mockRejectedValue(new LinuxIOError("bad yaml"));
    const endpoint = createEndpoint("docker", "validate_compose", {
      kind: "field",
      field: "content",
    });

    const { result } = renderJobAction(() =>
      endpoint.useAction({ error: "Validation failed" }),
    );

    act(() => result.current.mutate({ content: "nope" }));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(toast.error).toHaveBeenCalledWith("bad yaml", undefined);
  });
});

describe("useFetcher", () => {
  it("fetches through the query cache with the endpoint's key scheme", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue({ ok: true });
    const endpoint = createEndpoint<{ ok: boolean }>("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    const { result } = renderJobAction(() => endpoint.useFetcher());

    await expect(
      result.current("job-1", { staleTime: Infinity }),
    ).resolves.toEqual({ ok: true });
    // A second call inside staleTime is served from the cache.
    await expect(
      result.current("job-1", { staleTime: Infinity }),
    ).resolves.toEqual({ ok: true });
    expect(request).toHaveBeenCalledTimes(1);
    expect(request).toHaveBeenCalledWith(
      "jobs",
      "get",
      { jobId: "job-1" },
      { retryPolicy: "none", signal: expect.any(AbortSignal) },
    );
  });

  it("returns a referentially stable function", () => {
    const endpoint = createEndpoint("jobs", "get", {
      kind: "field",
      field: "jobId",
    });
    const { result, rerender } = renderJobAction(() => endpoint.useFetcher());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});

describe("useQueries", () => {
  it("composes one cached queryOptions object per input", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockImplementation((_handler, _command, wireRequest) =>
        Promise.resolve(wireRequest),
      );
    const endpoint = createEndpoint<{ jobId: string }>("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    const { result } = renderJobAction(() =>
      useQueries({
        queries: ["a", "b"].map((jobId) =>
          endpoint.queryOptions(jobId, { staleTime: Infinity }),
        ),
      }),
    );

    await waitFor(() =>
      expect(result.current.every((query) => query.isSuccess)).toBe(true),
    );
    expect(result.current.map((query) => query.data)).toEqual([
      { jobId: "a" },
      { jobId: "b" },
    ]);
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("honors the enabled option across every entry", () => {
    const request = vi.spyOn(core, "request").mockResolvedValue({});
    const endpoint = createEndpoint("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    renderJobAction(() =>
      useQueries({
        queries: [endpoint.queryOptions("a", { enabled: false })],
      }),
    );

    expect(request).not.toHaveBeenCalled();
  });
});

describe("useCache", () => {
  it("reads, writes, and clears entries through the endpoint key scheme", () => {
    const endpoint = createEndpoint<{ n: number }>("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    const { result } = renderJobAction(() => endpoint.useCache());

    result.current.set("job-1", { n: 1 });
    expect(result.current.get("job-1")).toEqual({ n: 1 });

    result.current.set("job-1", (old) => ({ n: (old?.n ?? 0) + 1 }));
    expect(result.current.get("job-1")).toEqual({ n: 2 });

    // No input targets every entry of the endpoint.
    result.current.remove();
    expect(result.current.get("job-1")).toBeUndefined();
  });

  it("invalidates one entry or the whole endpoint", async () => {
    const endpoint = createEndpoint("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    const { result, invalidateSpy } = renderJobAction(() =>
      endpoint.useCache(),
    );

    await result.current.invalidate("job-1");
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["linuxio", "jobs", "get", { jobId: "job-1" }],
    });

    await result.current.invalidate();
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["linuxio", "jobs", "get"],
    });
  });

  it("returns a referentially stable handle", () => {
    const endpoint = createEndpoint("jobs", "get", {
      kind: "field",
      field: "jobId",
    });
    const { result, rerender } = renderJobAction(() => endpoint.useCache());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});

describe("useJobStreamAction", () => {
  it("rejects query and duplex routes", () => {
    expect(() =>
      createEndpoint("system", "get_cpu_info", {
        kind: "none",
      }).useJobStreamAction(),
    ).toThrow(/not mutation\/job/);

    expect(() =>
      createEndpoint("container", "open", {
        kind: "object",
      }).useJobStreamAction(),
    ).toThrow(/not mutation\/job/);
  });

  it("attaches to the job stream, forwards progress, and completes through React Query", async () => {
    vi.spyOn(core, "request").mockResolvedValue(
      jobSnapshot({ state: "running" }),
    );
    const stream = createStream();
    vi.mocked(openJobAttachStream).mockReturnValue(stream);

    const onJobStart = vi.fn();
    const onOpen = vi.fn();
    const onProgress = vi.fn();
    const success = vi.fn();
    const endpoint = linuxio.docker.compose;
    const request = { action: "up", projectName: "web" };

    const { result, invalidateSpy } = renderJobAction(() =>
      endpoint.useJobStreamAction({
        invalidates: [["linuxio", "docker", "list_compose_projects"]],
        onJobStart,
        onOpen,
        onProgress: (progress, job, variables) => {
          const message: ComposeJobMessage = progress;
          onProgress(message, job, variables);
        },
        success,
      }),
    );
    let resolveInvalidation!: () => void;
    invalidateSpy.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveInvalidation = resolve;
        }),
    );

    act(() => result.current.mutate(request));
    await waitFor(() =>
      expect(onOpen).toHaveBeenCalledWith(
        stream,
        expect.objectContaining({ id: "job-1" }),
        request,
      ),
    );

    const progress = { message: "Pulling image", type: "progress" } as const;
    act(() => stream.onProgress?.(progress as never));
    expect(onProgress).toHaveBeenCalledWith(
      progress,
      expect.objectContaining({ id: "job-1" }),
      request,
    );

    const complete = { message: "done", type: "complete" } as const;
    act(() => stream.onResult?.({ data: complete, status: "ok" }));
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["linuxio", "docker", "list_compose_projects"],
      }),
    );
    expect(result.current.isPending).toBe(true);
    expect(success).not.toHaveBeenCalled();

    act(() => resolveInvalidation());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const terminalResult: ComposeJobResult | undefined = result.current.data;
    expect(terminalResult).toEqual(complete);
    expect(core.request).toHaveBeenCalledWith("docker", "compose", request, {
      retryPolicy: "none",
    });
    expect(openJobAttachStream).toHaveBeenCalledWith("job-1");
    expect(onJobStart).toHaveBeenCalledWith(
      expect.objectContaining({ id: "job-1" }),
      request,
    );
    expect(success).toHaveBeenCalledWith(complete, request);
  });
});
