import type { TaskSnapshot } from "@/api/generated/linuxio-types";

/**
 * Test double for a Mutation endpoint's `useMutation`, mirroring the real
 * lifecycle in `defineMutation` + `useActionMutation` + `mutationRequest` /
 * `watchMutation`:
 *
 *   onMutate -> start (watch: get) -> onStart(snapshot) -> progress frames
 *   -> success/error -> onSettled
 *
 * One stream carries the snapshot, progress and result, so there is no open
 * step. `watch(taskId)` adopts a running job and still reports its snapshot
 * through `onStart`, which is what gives a recovered job its id, and therefore a
 * working cancel. Aborting `signal` rejects the run with an AbortError, as the
 * real stream does whether `closeOnAbort` cancels or detaches. A function
 * `invalidates` is called, but no cache is invalidated, and string-form
 * `success`/`error` toasts are not reproduced.
 */

interface TaskSnapshotLike {
  id: string;
  state?: TaskSnapshot["state"];
}

export interface MutationJobTransport {
  closeOnAbort?: "abort" | "close";
  signal?: AbortSignal;
}

export interface MutationJobStream extends MutationJobTransport {
  /** The followed job: the started snapshot's id, or the id given to watch. */
  taskId: string;
  onProgress: (progress: unknown) => void;
}

export interface MutationJobMockConfig {
  closeOnAbort?: "abort" | "close";
  error?: string | ((error: unknown, variables: unknown) => void);
  invalidates?: unknown[] | ((result: unknown, variables: unknown) => unknown);
  onProgress?: (progress: unknown, task: unknown, variables: unknown) => void;
  onStart?: (task: unknown, variables: unknown) => void;
  options?: {
    onError?: (error: unknown, variables: unknown) => void;
    onMutate?: (variables: unknown) => void;
    onSettled?: () => void;
    onSuccess?: (result: unknown, variables: unknown) => void;
  };
  signal?: AbortSignal | ((variables: unknown) => AbortSignal | undefined);
  success?: string | ((result: unknown, variables: unknown) => void);
}

export interface MutationJobMockDeps {
  /** Mirrors the stream's `task.snapshot` frame; reject to fail the start. */
  start: (request: unknown) => Promise<TaskSnapshotLike>;
  /** Mirrors `tasks.get` on watch; defaults to a snapshot with only the id. */
  get?: (taskId: string) => Promise<TaskSnapshotLike>;
  /** The rest of the stream: progress frames, then the result or a rejection. */
  follow: (stream: MutationJobStream) => Promise<unknown>;
}

export interface MutationJobMock {
  watch: (taskId: string, variables: unknown) => void;
  mutate: (variables: unknown) => void;
  mutateAsync: (variables: unknown) => Promise<unknown>;
}

function abortError(): Error {
  const error = new Error("Operation cancelled");
  error.name = "AbortError";
  return error;
}

function aborted(signal: AbortSignal | undefined): Promise<never> {
  return new Promise((_resolve, reject) => {
    if (signal?.aborted) reject(abortError());
    else signal?.addEventListener("abort", () => reject(abortError()));
  });
}

export function createMutationJobMock(
  { follow, get, start }: MutationJobMockDeps,
  config?: MutationJobMockConfig,
): MutationJobMock {
  const run = async (request: unknown, adoptedTaskId?: string) => {
    config?.options?.onMutate?.(request);
    try {
      const transport: MutationJobTransport = {
        closeOnAbort: config?.closeOnAbort,
        signal:
          typeof config?.signal === "function"
            ? config.signal(request)
            : config?.signal,
      };
      const job = async () => {
        const task =
          adoptedTaskId === undefined
            ? await start(request)
            : await (get?.(adoptedTaskId) ?? { id: adoptedTaskId });
        // An aborted stream delivers no further frames.
        if (transport.signal?.aborted) throw abortError();
        config?.onStart?.(task, request);
        return follow({
          ...transport,
          taskId: task.id,
          onProgress: (progress) =>
            config?.onProgress?.(progress, task, request),
        });
      };
      const result = await Promise.race([job(), aborted(transport.signal)]);

      if (typeof config?.invalidates === "function") {
        config.invalidates(result, request);
      }
      if (typeof config?.success === "function") {
        config.success(result, request);
      }
      config?.options?.onSuccess?.(result, request);
      return result;
    } catch (error) {
      if (typeof config?.error === "function") config.error(error, request);
      config?.options?.onError?.(error, request);
      throw error;
    } finally {
      config?.options?.onSettled?.();
    }
  };

  return {
    watch: (taskId, variables) => {
      void run(variables, taskId).catch(() => undefined);
    },
    mutate: (variables) => {
      void run(variables).catch(() => undefined);
    },
    mutateAsync: (variables) => run(variables),
  };
}
