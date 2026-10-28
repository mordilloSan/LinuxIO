import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { TaskSnapshot } from "@/api/generated/linuxio-types";
import { mutationRequest, startJob } from "@/api/linuxio-core";
import { defineMutation } from "@/api/mutation-react-query";
import { OPERATION_QUERY_INVALIDATIONS } from "@/api/operation-query-invalidations";
import type { ProgressFrame, Stream } from "@/api/StreamMultiplexer";

const muxMocks = vi.hoisted(() => ({
  getStreamMux: vi.fn(),
  initStreamMux: vi.fn(),
  waitForStreamMux: vi.fn(),
}));

vi.mock("@/api/StreamMultiplexer", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/StreamMultiplexer")>()),
  ...muxMocks,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

const UUID =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const REQUEST = { containerId: "abc" };
const PROGRESS = { message: "pulling" } as unknown as ProgressFrame;

function createStream(): Stream {
  return {
    abort: vi.fn(),
    close: vi.fn(),
    id: 1,
    onClose: null,
    onData: null,
    onProgress: null,
    onResult: null,
    resize: vi.fn(),
    status: "open",
    type: "request",
    write: vi.fn(),
  };
}

/** Install an open mux whose openStream hands out `streams` in order. */
function installMux(...streams: Stream[]) {
  const openStream = vi.fn(
    (_type: string, _payload?: Uint8Array): Stream | null =>
      streams.shift() ?? null,
  );
  const mux = { openStream, status: "open" };
  muxMocks.getStreamMux.mockReturnValue(mux);
  return mux;
}

function opened(mux: ReturnType<typeof installMux>, index: number) {
  const [type, payload] = mux.openStream.mock.calls[index];
  return {
    type,
    envelope: JSON.parse(new TextDecoder().decode(payload)) as unknown,
  };
}

function runningTask(id: string): TaskSnapshot {
  return {
    created_at: "2026-10-26T00:00:00Z",
    id,
    state: "running",
    type: "docker.update_container",
    updated_at: "2026-10-26T00:00:00Z",
  };
}

function snapshotFrame(task: TaskSnapshot): ProgressFrame {
  return { type: "task.snapshot", task } as unknown as ProgressFrame;
}

async function whenBound(stream: Stream) {
  await vi.waitFor(() => expect(stream.onResult).not.toBeNull());
}

function renderQueryHook<T>(useHook: () => T) {
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

describe("mutationRequest", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    muxMocks.waitForStreamMux.mockResolvedValue(true);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("sends the envelope with a uuid id and no timeout", async () => {
    vi.useFakeTimers();
    const stream = createStream();
    const mux = installMux(stream);
    let settled = false;

    const pending = mutationRequest(
      "docker",
      "update_container",
      REQUEST,
      {},
    ).finally(() => {
      settled = true;
    });
    await vi.advanceTimersByTimeAsync(0);

    expect(opened(mux, 0)).toEqual({
      type: "docker.update_container",
      envelope: {
        route: "docker.update_container",
        request: REQUEST,
        id: expect.stringMatching(UUID),
      },
    });

    await vi.advanceTimersByTimeAsync(60 * 60_000);
    expect(settled).toBe(false);
    expect(stream.abort).not.toHaveBeenCalled();
    expect(stream.close).not.toHaveBeenCalled();

    stream.onResult?.({ status: "ok", data: { updated: true } });
    await expect(pending).resolves.toEqual({ updated: true });
  });

  it("delivers snapshot, progress and result", async () => {
    const stream = createStream();
    installMux(stream);
    const onSnapshot = vi.fn();
    const onProgress = vi.fn();

    const pending = mutationRequest("docker", "update_container", REQUEST, {
      onSnapshot,
      onProgress,
    });
    await whenBound(stream);
    const task = runningTask("job-1");
    stream.onProgress?.(snapshotFrame(task));
    stream.onProgress?.(PROGRESS);
    stream.onResult?.({ status: "ok", data: { updated: true } });

    await expect(pending).resolves.toEqual({ updated: true });
    expect(onSnapshot).toHaveBeenCalledExactlyOnceWith(task);
    expect(onProgress).toHaveBeenCalledExactlyOnceWith(PROGRESS);
  });

  it("rejects an error frame without resending", async () => {
    const stream = createStream();
    const mux = installMux(stream, createStream());

    const pending = mutationRequest("docker", "update_container", REQUEST, {});
    await whenBound(stream);
    stream.onResult?.({ status: "error", error: "image missing", code: 404 });

    await expect(pending).rejects.toMatchObject({
      message: "image missing",
      code: 404,
    });
    expect(mux.openStream).toHaveBeenCalledTimes(1);
  });

  it("resends once with the same id when the stream closes before the snapshot", async () => {
    const first = createStream();
    const second = createStream();
    const mux = installMux(first, second);

    const pending = mutationRequest("docker", "update_container", REQUEST, {});
    await whenBound(first);
    first.onClose?.();
    await whenBound(second);

    expect(muxMocks.waitForStreamMux).toHaveBeenCalledTimes(2);
    expect(opened(mux, 1)).toEqual(opened(mux, 0));
    second.onProgress?.(snapshotFrame(runningTask("job-1")));
    second.onResult?.({ status: "ok", data: 1 });
    await expect(pending).resolves.toBe(1);
  });

  it("reattaches with tasks.watch when the stream closes after the snapshot", async () => {
    const first = createStream();
    const watch = createStream();
    const mux = installMux(first, watch);
    const onProgress = vi.fn();

    const pending = mutationRequest("docker", "update_container", REQUEST, {
      onProgress,
    });
    await whenBound(first);
    first.onProgress?.(snapshotFrame(runningTask("job-1")));
    first.onClose?.();
    await whenBound(watch);

    const { id } = opened(mux, 0).envelope as { id: string };
    expect(opened(mux, 1)).toEqual({
      type: "tasks.watch",
      envelope: { route: "tasks.watch", request: { taskId: id } },
    });
    watch.onProgress?.(PROGRESS);
    watch.onResult?.({ status: "ok", data: 2 });
    await expect(pending).resolves.toBe(2);
    expect(onProgress).toHaveBeenCalledExactlyOnceWith(PROGRESS);
  });

  it("reattaches once after a resend that reached the job", async () => {
    const first = createStream();
    const resend = createStream();
    const watch = createStream();
    const mux = installMux(first, resend, watch);

    const pending = mutationRequest("docker", "update_container", REQUEST, {});
    await whenBound(first);
    first.onClose?.();
    await whenBound(resend);
    resend.onProgress?.(snapshotFrame(runningTask("job-1")));
    resend.onClose?.();
    await whenBound(watch);

    expect(opened(mux, 2).type).toBe("tasks.watch");
    watch.onResult?.({ status: "ok", data: 3 });
    await expect(pending).resolves.toBe(3);
  });

  it("rejects outcome_unknown when the reattached watch is lost too", async () => {
    const first = createStream();
    const watch = createStream();
    const mux = installMux(first, watch, createStream());

    const pending = mutationRequest("docker", "update_container", REQUEST, {});
    await whenBound(first);
    first.onProgress?.(snapshotFrame(runningTask("job-1")));
    first.onClose?.();
    await whenBound(watch);
    watch.onClose?.();

    await expect(pending).rejects.toMatchObject({ code: "outcome_unknown" });
    expect(mux.openStream).toHaveBeenCalledTimes(2);
  });

  it("rejects outcome_unknown after a second loss", async () => {
    const first = createStream();
    const second = createStream();
    const mux = installMux(first, second, createStream());

    const pending = mutationRequest("docker", "update_container", REQUEST, {});
    await whenBound(first);
    first.onClose?.();
    await whenBound(second);
    second.onClose?.();

    await expect(pending).rejects.toMatchObject({ code: "outcome_unknown" });
    expect(mux.openStream).toHaveBeenCalledTimes(2);
  });

  it("rejects the original error when the request was never sent", async () => {
    const mux = installMux();

    await expect(
      mutationRequest("docker", "update_container", REQUEST, {}),
    ).rejects.toMatchObject({ code: "connection_unavailable" });
    expect(mux.openStream).toHaveBeenCalledTimes(2);
  });

  it("aborts the stream when the signal fires", async () => {
    const stream = createStream();
    installMux(stream);
    const controller = new AbortController();

    const pending = mutationRequest("docker", "update_container", REQUEST, {
      signal: controller.signal,
    });
    await whenBound(stream);
    controller.abort();

    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(stream.abort).toHaveBeenCalledTimes(1);
  });
});

describe("startJob", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    muxMocks.waitForStreamMux.mockResolvedValue(true);
  });

  it("resolves with the first snapshot and detaches the stream", async () => {
    const stream = createStream();
    const mux = installMux(stream);

    const pending = startJob("docker", "compose", REQUEST);
    await whenBound(stream);
    expect(opened(mux, 0).envelope).toEqual({
      route: "docker.compose",
      request: REQUEST,
      id: expect.stringMatching(UUID),
    });
    const task = runningTask("job-1");
    stream.onProgress?.(snapshotFrame(task));

    await expect(pending).resolves.toEqual(task);
    expect(stream.close).toHaveBeenCalledTimes(1);
    expect(stream.abort).not.toHaveBeenCalled();
  });

  it("resends once with the same id when the stream closes before the snapshot", async () => {
    const first = createStream();
    const second = createStream();
    const mux = installMux(first, second);

    const pending = startJob("docker", "compose", REQUEST);
    await whenBound(first);
    first.onClose?.();
    await whenBound(second);
    expect(opened(mux, 1)).toEqual(opened(mux, 0));
    const task = runningTask("job-1");
    second.onProgress?.(snapshotFrame(task));

    await expect(pending).resolves.toEqual(task);
  });

  it("rejects a refusal without resending", async () => {
    const stream = createStream();
    const mux = installMux(stream, createStream());

    const pending = startJob("docker", "compose", REQUEST);
    await whenBound(stream);
    stream.onResult?.({ status: "error", error: "already running", code: 409 });

    await expect(pending).rejects.toMatchObject({ code: 409 });
    expect(mux.openStream).toHaveBeenCalledTimes(1);
  });
});

describe("defineMutation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    muxMocks.waitForStreamMux.mockResolvedValue(true);
  });

  it("useMutation invalidates the manifest on success", async () => {
    const stream = createStream();
    installMux(stream);
    const endpoint = defineMutation<{ updated: boolean }>(
      "docker",
      "update_container",
      { kind: "object" },
    );
    const onStart = vi.fn();
    const onProgress = vi.fn();
    const { result, invalidateSpy } = renderQueryHook(() =>
      endpoint.useMutation({ onStart, onProgress }),
    );

    act(() => result.current.mutate(REQUEST));
    await whenBound(stream);
    const task = runningTask("job-1");
    stream.onProgress?.(snapshotFrame(task));
    stream.onProgress?.(PROGRESS);
    stream.onResult?.({ status: "ok", data: { updated: true } });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const manifestKeys =
      OPERATION_QUERY_INVALIDATIONS["docker.update_container"];
    for (const queryKey of manifestKeys) {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey });
    }
    expect(invalidateSpy).toHaveBeenCalledTimes(manifestKeys.length);
    expect(onStart).toHaveBeenCalledExactlyOnceWith(task, REQUEST);
    expect(onProgress).toHaveBeenCalledExactlyOnceWith(PROGRESS, task, REQUEST);
  });

  it("watch adopts a running job through tasks.watch", async () => {
    const get = createStream();
    const watch = createStream();
    const mux = installMux(get, watch);
    const endpoint = defineMutation<number>("docker", "update_container", {
      kind: "object",
    });
    const onStart = vi.fn();
    const { result } = renderQueryHook(() => endpoint.useMutation({ onStart }));

    act(() => result.current.watch("job-7", REQUEST));
    await whenBound(get);
    const task = runningTask("job-7");
    get.onResult?.({ status: "ok", data: task });
    await whenBound(watch);
    watch.onResult?.({ status: "ok", data: 7 });
    await waitFor(() => expect(result.current.data).toBe(7));

    expect(opened(mux, 0).envelope).toEqual({
      route: "tasks.get",
      request: { taskId: "job-7" },
    });
    expect(opened(mux, 1).envelope).toEqual({
      route: "tasks.watch",
      request: { taskId: "job-7" },
    });
    expect(onStart).toHaveBeenCalledExactlyOnceWith(task, REQUEST);
  });
});
