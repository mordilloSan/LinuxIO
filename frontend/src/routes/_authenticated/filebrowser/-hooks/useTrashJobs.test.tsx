import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FileBatchResult, TrashEntry, TrashRestoreRequest } from "@/api";
import type { MutationHandlers } from "@/api/linuxio-core";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { act, createTestQueryClient, renderHook, waitFor } from "@/test/render";

import {
  nextTakenNamesPrompt,
  settleTakenNamesPrompt,
} from "./takenNamesPrompts";
import type { ResolveCollisionsFn } from "./useFileConflicts";
import { useTrashJobs } from "./useTrashJobs";

// The generated endpoints and TanStack's useMutation are real; only the job
// stream is faked, and each job runs until the test ends it.
interface FakeJob {
  command: string;
  request: TrashRestoreRequest;
  resolve: (result: FileBatchResult) => void;
}

const mocks = vi.hoisted(() => ({
  jobs: [] as FakeJob[],
  request: vi.fn(),
  toast: { error: vi.fn(), info: vi.fn(), success: vi.fn() },
}));

vi.mock("@/api/linuxio-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/linuxio-core")>()),
  // The incoming side of the prompt; the prompt works without it.
  request: mocks.request,
  mutationRequest: (
    _handler: string,
    command: string,
    request: TrashRestoreRequest,
    handlers: MutationHandlers<unknown> = {},
  ) =>
    new Promise((resolve) => {
      const id = `${command}-${mocks.jobs.length + 1}`;
      mocks.jobs.push({ command, request, resolve });
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
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks.toast,
}));

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={createTestQueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

const TRASH = "/home/alice/.local/share/Trash/files";
const entry = (
  name: string,
  originalPath: string,
  extra: Partial<TrashEntry> = {},
): TrashEntry => ({
  confirmRestore: false,
  deletedAt: "2026-11-01T10:00:00Z",
  isDir: false,
  name,
  originalPath,
  path: `${TRASH}/${name}`,
  size: 10,
  ...extra,
});
const A = entry("a.txt", "/home/alice/docs/a.txt");
const B = entry("b.txt", "/home/alice/docs/b.txt");
const C = entry("c.txt", "/home/alice/c.txt");

const taken = (path: string) => ({
  error: "file exists",
  path,
  reason: "exists" as const,
});

const renderJobs = (resolveCollisions: ResolveCollisionsFn) =>
  renderHook(() => useTrashJobs({ resolveCollisions }), { wrapper });

const endJob = async (index: number, result: FileBatchResult) => {
  await waitFor(() => expect(mocks.jobs.length).toBeGreaterThan(index));
  await act(async () => {
    mocks.jobs[index].resolve(result);
  });
};

describe("useTrashJobs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.jobs = [];
    mocks.request.mockRejectedValue(new Error("offline"));
    // The questions wait outside the page, so none carries over.
    for (
      let prompt = nextTakenNamesPrompt();
      prompt;
      prompt = nextTakenNamesPrompt()
    ) {
      settleTakenNamesPrompt(prompt.id);
    }
  });

  it("asks about taken names with the paste prompt and sends those items again", async () => {
    const resolveCollisions = vi.fn(
      async <T,>(items: T[], getDestPath: (item: T) => string) => {
        expect(items.map(getDestPath)).toEqual([
          A.originalPath,
          B.originalPath,
        ]);
        // Replace a.txt, keep both for b.txt.
        return { keepBoth: [items[1]], kept: items, overwrite: true };
      },
    ) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() =>
      result.current.restore({ paths: [A.path, B.path, C.path] }, [A, B, C]),
    );
    await endJob(0, {
      failed: [
        taken(A.path),
        taken(B.path),
        { error: "permission denied", path: C.path },
      ],
      succeeded: 0,
      total: 3,
    });

    // The other failure is reported at once; the taken names are asked about
    // in the folder they share.
    expect(mocks.toast.error).toHaveBeenCalledWith("Failed to restore 1 item", {
      description: "c.txt: permission denied",
    });
    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(resolveCollisions).toHaveBeenCalledWith(
      [A.path, B.path],
      expect.any(Function),
      "/home/alice/docs",
      expect.any(Function),
    );
    // The replace is confirmed for the destination the user was asked about.
    expect(mocks.jobs[1].request).toEqual({
      confirmedDestinations: { [A.path]: A.originalPath },
      keepBoth: [B.path],
      overwrite: true,
      paths: [A.path, B.path],
    });
    // The page shows and asks at once, so nothing says the items wait.
    expect(mocks.toast.info).not.toHaveBeenCalled();
    // Both jobs are reported by the page, not again by the task tray.
    expect(markTerminalFeedbackEmitted("trash_restore-1")).toBe(false);
    expect(markTerminalFeedbackEmitted("trash_restore-2")).toBe(false);
  });

  it("leaves skipped items out and sends no overwrite for keep both alone", async () => {
    const resolveCollisions = vi.fn(async <T,>(items: T[]) => ({
      keepBoth: [items[1]],
      kept: [items[1]],
      overwrite: false,
    })) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() => result.current.restore({ paths: [A.path, B.path] }, [A, B]));
    await endJob(0, {
      failed: [taken(A.path), taken(B.path)],
      succeeded: 0,
      total: 2,
    });

    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(mocks.jobs[1].request).toEqual({
      keepBoth: [B.path],
      paths: [B.path],
    });
  });

  it("replaces with only one of two items that go back under one name", async () => {
    const older = entry("a.txt.2", A.originalPath);
    const resolveCollisions = vi.fn(async <T,>(items: T[]) => ({
      keepBoth: [],
      kept: items,
      overwrite: true,
    })) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() =>
      result.current.restore({ paths: [A.path, older.path] }, [A, older]),
    );
    await endJob(0, {
      failed: [taken(A.path), taken(older.path)],
      succeeded: 0,
      total: 2,
    });

    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(mocks.jobs[1].request).toEqual({
      confirmedDestinations: { [A.path]: A.originalPath },
      overwrite: true,
      paths: [A.path],
    });
    expect(mocks.toast.info).toHaveBeenCalledWith(
      "1 item with the same name stays in the Trash",
    );
  });

  it("sends the confirmed destination again for an item another program trashed", async () => {
    const foreign = entry("app.conf", "/etc/app.d/app.conf", {
      confirmRestore: true,
    });
    const resolveCollisions = vi.fn(async <T,>(items: T[]) => ({
      keepBoth: [],
      kept: items,
      overwrite: true,
    })) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() =>
      result.current.restore(
        {
          confirmedDestinations: { [foreign.path]: foreign.originalPath },
          paths: [foreign.path, A.path],
        },
        [foreign, A],
      ),
    );
    await endJob(0, {
      failed: [taken(foreign.path)],
      succeeded: 1,
      total: 2,
    });

    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(mocks.toast.success).toHaveBeenCalledWith("Restored 1 item");
    expect(mocks.jobs[1].request).toEqual({
      confirmedDestinations: { [foreign.path]: foreign.originalPath },
      overwrite: true,
      paths: [foreign.path],
    });
  });

  it("keeps the folder and names of a restore into a chosen folder", async () => {
    const resolveCollisions = vi.fn(
      async <T,>(items: T[], getDestPath: (item: T) => string) => {
        expect(items.map(getDestPath)).toEqual(["/srv/shared/notes.txt"]);
        return { keepBoth: [], kept: items, overwrite: true };
      },
    ) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() =>
      result.current.restoreInto(
        [
          { name: "notes.txt", path: A.path },
          { name: "b.txt", path: B.path },
        ],
        "/srv/shared/",
      ),
    );
    await waitFor(() => expect(mocks.jobs).toHaveLength(1));
    expect(mocks.jobs[0].request).toEqual({
      destination: "/srv/shared",
      names: { [A.path]: "notes.txt", [B.path]: "b.txt" },
      paths: [A.path, B.path],
    });
    await endJob(0, { failed: [taken(A.path)], succeeded: 1, total: 2 });

    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(resolveCollisions).toHaveBeenCalledWith(
      [A.path],
      expect.any(Function),
      "/srv/shared",
      expect.any(Function),
    );
    expect(mocks.jobs[1].request).toEqual({
      destination: "/srv/shared",
      names: { [A.path]: "notes.txt" },
      overwrite: true,
      paths: [A.path],
    });
  });

  it("reports a name taken again on the second request instead of asking again", async () => {
    const resolveCollisions = vi.fn(async <T,>(items: T[]) => ({
      keepBoth: [],
      kept: items,
      overwrite: true,
    })) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() => result.current.restore({ paths: [A.path] }, [A]));
    await endJob(0, { failed: [taken(A.path)], succeeded: 0, total: 1 });
    await endJob(1, { failed: [taken(A.path)], succeeded: 0, total: 1 });

    expect(resolveCollisions).toHaveBeenCalledTimes(1);
    expect(mocks.jobs).toHaveLength(2);
    expect(mocks.toast.error).toHaveBeenCalledWith("Failed to restore 1 item", {
      description: "a.txt: file exists",
    });
  });

  it("leaves the items in the Trash when the prompt is cancelled", async () => {
    const resolveCollisions = vi.fn(
      async () => null,
    ) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() => result.current.restore({ paths: [A.path] }, [A]));
    await endJob(0, { failed: [taken(A.path)], succeeded: 0, total: 1 });

    await waitFor(() => expect(resolveCollisions).toHaveBeenCalledTimes(1));
    expect(mocks.jobs).toHaveLength(1);
    expect(mocks.toast.error).not.toHaveBeenCalled();
  });

  it("asks about two restores one after the other", async () => {
    const answers: ((value: unknown) => void)[] = [];
    const resolveCollisions = vi.fn(
      (items: unknown[]) =>
        new Promise((resolve) => {
          answers.push(() =>
            resolve({ keepBoth: [], kept: items, overwrite: true }),
          );
        }),
    ) as unknown as ResolveCollisionsFn;
    const { result } = renderJobs(resolveCollisions);

    act(() => result.current.restore({ paths: [A.path] }, [A]));
    act(() => result.current.restore({ paths: [B.path] }, [B]));
    await endJob(0, { failed: [taken(A.path)], succeeded: 0, total: 1 });
    await endJob(1, { failed: [taken(B.path)], succeeded: 0, total: 1 });

    // The prompt holds one question: the second waits for the first.
    await waitFor(() => expect(resolveCollisions).toHaveBeenCalledTimes(1));
    await act(async () => answers[0](undefined));
    await waitFor(() => expect(resolveCollisions).toHaveBeenCalledTimes(2));
    await act(async () => answers[1](undefined));
    await waitFor(() => expect(mocks.jobs).toHaveLength(4));
    expect(mocks.jobs[2].request.paths).toEqual([A.path]);
    expect(mocks.jobs[3].request.paths).toEqual([B.path]);
  });

  it("keeps the question about taken names until a file browser shows again", async () => {
    const leftPrompt = vi.fn() as unknown as ResolveCollisionsFn;
    const { result, unmount } = renderJobs(leftPrompt);

    act(() => result.current.restore({ paths: [A.path, B.path] }, [A, B]));
    await waitFor(() => expect(mocks.jobs).toHaveLength(1));
    // The page that started it, and its prompt, are gone.
    unmount();
    await endJob(0, { failed: [taken(A.path)], succeeded: 1, total: 2 });

    expect(leftPrompt).not.toHaveBeenCalled();
    expect(mocks.toast.error).not.toHaveBeenCalled();
    expect(mocks.toast.success).toHaveBeenCalledWith("Restored 1 item");

    // The file browser shows again: its prompt asks, and the answer is sent.
    const resolveCollisions = vi.fn(
      async <T,>(items: T[], getDestPath: (item: T) => string) => {
        expect(items.map(getDestPath)).toEqual([A.originalPath]);
        return { keepBoth: items, kept: items, overwrite: false };
      },
    ) as unknown as ResolveCollisionsFn;
    renderJobs(resolveCollisions);

    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(resolveCollisions).toHaveBeenCalledWith(
      [A.path],
      expect.any(Function),
      "/home/alice/docs",
      expect.any(Function),
    );
    expect(mocks.jobs[1].request).toEqual({
      keepBoth: [A.path],
      paths: [A.path],
    });
  });

  it("says that items wait when their names are all taken and no file browser shows", async () => {
    const { result, unmount } = renderJobs(vi.fn() as ResolveCollisionsFn);

    act(() => result.current.restore({ paths: [A.path, B.path] }, [A, B]));
    await waitFor(() => expect(mocks.jobs).toHaveLength(1));
    unmount();
    await endJob(0, {
      failed: [taken(A.path), taken(B.path)],
      succeeded: 0,
      total: 2,
    });

    expect(mocks.toast.info).toHaveBeenCalledWith("2 items wait in the Trash", {
      description:
        "Their names are taken where they go. The file browser asks what to do with them when it shows.",
    });
    expect(mocks.toast.success).not.toHaveBeenCalled();
    expect(mocks.toast.error).not.toHaveBeenCalled();
  });

  it("asks again on the next page when the page closes during the question", async () => {
    const unanswered = vi.fn(
      () => new Promise(() => {}),
    ) as unknown as ResolveCollisionsFn;
    const { result, unmount } = renderJobs(unanswered);

    act(() => result.current.restore({ paths: [A.path] }, [A]));
    await endJob(0, { failed: [taken(A.path)], succeeded: 0, total: 1 });
    await waitFor(() => expect(unanswered).toHaveBeenCalledTimes(1));
    unmount();

    const resolveCollisions = vi.fn(async <T,>(items: T[]) => ({
      keepBoth: [],
      kept: items,
      overwrite: true,
    })) as unknown as ResolveCollisionsFn;
    renderJobs(resolveCollisions);

    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(resolveCollisions).toHaveBeenCalledTimes(1);
    expect(mocks.jobs[1].request).toEqual({
      confirmedDestinations: { [A.path]: A.originalPath },
      overwrite: true,
      paths: [A.path],
    });
  });

  it("purges and reports each job with its own result", async () => {
    const { result } = renderJobs(vi.fn());

    act(() => result.current.purge([A.path]));
    act(() => result.current.purge([B.path, C.path]));
    await waitFor(() => expect(mocks.jobs).toHaveLength(2));
    expect(mocks.jobs.map((job) => [job.command, job.request])).toEqual([
      ["trash_purge", { paths: [A.path] }],
      ["trash_purge", { paths: [B.path, C.path] }],
    ]);
    await endJob(1, { failed: [], succeeded: 2, total: 2 });
    await endJob(0, {
      failed: [{ error: "busy", path: A.path }],
      succeeded: 0,
      total: 1,
    });

    expect(mocks.toast.success).toHaveBeenCalledWith("Deleted 2 items");
    expect(mocks.toast.error).toHaveBeenCalledWith("Failed to delete 1 item", {
      description: "a.txt: busy",
    });
  });
});
