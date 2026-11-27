import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { type FileBatchResult, LinuxIOError, TASK_CANCELED_CODE } from "@/api";
import type { MutationHandlers } from "@/api/linuxio-core";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { act, createTestQueryClient, renderHook, waitFor } from "@/test/render";

import { useFileMutations } from "./useFileMutations";

// Unlike useFileMutations.test.tsx, the generated endpoints and TanStack's
// useMutation are real here: only the job stream is faked, and each job runs
// until the test ends it. That is what shows how one useMutation behaves
// when it is asked for a second job while the first still runs.
interface FakeJob {
  command: string;
  reject: (error: unknown) => void;
  request: { paths: string[] };
  resolve: (result: FileBatchResult) => void;
}

const mocks = vi.hoisted(() => ({
  invalidateListing: vi.fn(),
  jobs: [] as FakeJob[],
  toast: { error: vi.fn(), info: vi.fn(), success: vi.fn() },
}));

vi.mock("@/api/linuxio-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/linuxio-core")>()),
  mutationRequest: (
    _handler: string,
    command: string,
    request: { paths: string[] },
    handlers: MutationHandlers<unknown> = {},
  ) =>
    new Promise((resolve, reject) => {
      const id = `${command}-${mocks.jobs.length + 1}`;
      mocks.jobs.push({ command, reject, request, resolve });
      handlers.onSnapshot?.({
        created_at: "2026-11-16T00:00:00Z",
        id,
        state: "running",
        type: `filebrowser.${command}`,
        updated_at: "2026-11-16T00:00:00Z",
      });
    }),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => vi.fn(),
  useRouter: () => ({ state: { location: { pathname: "/filebrowser" } } }),
}));

vi.mock("./useListingInvalidation", () => ({
  useListingInvalidation: () => mocks.invalidateListing,
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks.toast,
}));

vi.mock("@/tasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({}),
}));

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={createTestQueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

const A = "/srv/projects/a.txt";
const B = "/srv/projects/b.txt";
const C = "/srv/projects/c.txt";

describe("useFileMutations with several jobs of one kind", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.jobs = [];
  });

  it("runs two moves to Trash at once and reports each with its own request", async () => {
    const onDeleteSuccess = vi.fn();
    const { result } = renderHook(
      () =>
        useFileMutations({ normalizedPath: "/srv/projects", onDeleteSuccess }),
      { wrapper },
    );

    let first!: Promise<FileBatchResult | undefined>;
    let second!: Promise<FileBatchResult | undefined>;
    act(() => {
      first = result.current.trashItems([A]);
    });
    act(() => {
      second = result.current.trashItems([B, C]);
    });
    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(mocks.jobs.map((job) => job.request)).toEqual([
      { paths: [A] },
      { paths: [B, C] },
    ]);
    // Their items leave the selection as each starts, so a second Delete
    // does not send what a running job is already removing.
    expect(onDeleteSuccess.mock.calls).toEqual([[[A]], [[B, C]]]);
    onDeleteSuccess.mockClear();
    // Each job is marked reported as it starts, so the task tray does not
    // report it a second time.
    expect(markTerminalFeedbackEmitted("trash_batch-1")).toBe(false);
    expect(markTerminalFeedbackEmitted("trash_batch-2")).toBe(false);

    // The later job ends first; then the earlier one, with failures.
    await act(async () => {
      mocks.jobs[1].resolve({ failed: [], succeeded: 2, total: 2 });
      await second;
    });
    expect(onDeleteSuccess.mock.calls).toEqual([[[B, C]]]);
    expect(mocks.toast.success).toHaveBeenCalledWith("Moved 2 items to Trash", {
      action: expect.objectContaining({ label: "Open Trash" }),
    });

    await act(async () => {
      mocks.jobs[0].resolve({
        failed: [{ error: "permission denied", path: A }],
        succeeded: 0,
        total: 1,
      });
      await first;
    });
    expect(onDeleteSuccess.mock.calls).toEqual([[[B, C]], [[A]]]);
    expect(mocks.toast.error).toHaveBeenCalledWith(
      "Failed to move 1 item to Trash",
      { description: "a.txt: permission denied" },
    );
    expect(mocks.toast.success).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateListing).toHaveBeenCalledTimes(2);
  });

  it("does not toast a cancelled delete while another one reports", async () => {
    const onDeleteSuccess = vi.fn();
    const { result } = renderHook(
      () =>
        useFileMutations({ normalizedPath: "/srv/projects", onDeleteSuccess }),
      { wrapper },
    );

    let cancelled!: Promise<FileBatchResult | undefined>;
    let finished!: Promise<FileBatchResult | undefined>;
    act(() => {
      cancelled = result.current.deleteItems([A]);
    });
    act(() => {
      finished = result.current.deleteItems([B]);
    });
    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    // Both jobs took their items out of the selection as they started.
    onDeleteSuccess.mockClear();

    await act(async () => {
      mocks.jobs[0].reject(
        new LinuxIOError("operation aborted", TASK_CANCELED_CODE),
      );
      await cancelled.catch(() => {});
    });
    // A cancelled delete has removed some of its items: the listing is read
    // again and its own items leave the selection, with no failure toast.
    expect(mocks.toast.error).not.toHaveBeenCalled();
    expect(onDeleteSuccess.mock.calls).toEqual([[[A]]]);
    expect(mocks.invalidateListing).toHaveBeenCalledTimes(1);

    await act(async () => {
      mocks.jobs[1].resolve({ failed: [], succeeded: 1, total: 1 });
      await finished;
    });
    expect(onDeleteSuccess.mock.calls).toEqual([[[A]], [[B]]]);
    expect(mocks.toast.success).toHaveBeenCalledWith(
      "Items deleted successfully",
    );
    expect(mocks.toast.error).not.toHaveBeenCalled();
  });
});
