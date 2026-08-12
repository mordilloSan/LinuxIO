// Imported from the leaf modules rather than "@/api": test files that mock the
// "@/api" barrel pull this helper in from inside that mock factory, and going
// through the barrel would re-enter it.
import type { TaskError, TaskSnapshot } from "@/api/generated/linuxio-types";
import { LinuxIOError } from "@/api/linuxio-core";
import { isTerminalTaskState } from "@/api/task-state";

/**
 * Test double for `useTaskStreamAction`, mirroring the real lifecycle in
 * `waitForTaskStreamAction` + `useActionMutation`:
 *
 *   onMutate -> submit -> onTaskStart -> (terminal snapshot short-circuit)
 *   -> openStream -> onOpen -> progress frames -> success/error -> onSettled
 *
 * Hand-rolled mocks kept drifting from that order — most importantly by never
 * firing `onSettled` (which owns per-run ref cleanup) and by skipping
 * `onTaskStart` on `watch` (which is what gives a recovered task its task id, and
 * therefore a working cancel). Tests drive the stream through `runStream` and
 * assert against the same callback order production code sees.
 */

interface TaskSnapshotLike {
  error?: TaskError;
  id: string;
  progress?: unknown;
  result?: unknown;
  state?: TaskSnapshot["state"];
}

interface StreamHandlers {
  onProgress: (progress: unknown) => void;
}

export interface TaskStreamActionMockConfig {
  error?: (error: unknown, variables: unknown) => unknown;
  onTaskStart?: (task: unknown, variables: unknown) => void;
  onOpen?: (stream: unknown, task: unknown, variables: unknown) => void;
  onProgress?: (progress: unknown, task: unknown, variables: unknown) => void;
  openErrorMessage?: string;
  options?: {
    onError?: (error: unknown, variables: unknown) => void;
    onMutate?: (variables: unknown) => void;
    onSettled?: () => void;
    onSuccess?: (result: unknown, variables: unknown) => void;
  };
  success?: (result: unknown, variables: unknown) => unknown;
}

export interface TaskStreamActionMockDeps {
  /** Mirrors `openTaskWatchStream`; a falsy return simulates an open failure. */
  openStream: (taskId: string) => unknown;
  /** Mirrors `waitForStreamResult`: resolve with the result, reject to fail. */
  runStream: (stream: unknown, handlers: StreamHandlers) => Promise<unknown>;
  /** Mirrors the create call; resolves the task snapshot. */
  submit: (request: unknown) => Promise<TaskSnapshotLike>;
}

export interface TaskStreamActionMock {
  watch: (task: TaskSnapshotLike, variables: unknown) => void;
  mutate: (variables: unknown) => void;
  mutateAsync: (variables: unknown) => Promise<unknown>;
}

export function createTaskStreamActionMock(
  { openStream, runStream, submit }: TaskStreamActionMockDeps,
  config?: TaskStreamActionMockConfig,
): TaskStreamActionMock {
  const run = async (request: unknown, watchedTask?: TaskSnapshotLike) => {
    config?.options?.onMutate?.(request);
    try {
      const task = watchedTask ?? (await submit(request));
      config?.onTaskStart?.(task, request);

      let result: unknown;
      if (task.state !== undefined && isTerminalTaskState(task.state)) {
        if (task.progress !== undefined && task.progress !== null) {
          config?.onProgress?.(task.progress, task, request);
        }
        if (task.state !== "completed") {
          throw new LinuxIOError(
            task.error?.message ?? "Task failed",
            task.error?.code,
          );
        }
        result = task.result;
      } else {
        const stream = openStream(task.id);
        if (!stream) {
          throw new LinuxIOError(
            config?.openErrorMessage ?? "Failed to watch task stream",
            "stream_unavailable",
          );
        }
        config?.onOpen?.(stream, task, request);
        result = await runStream(stream, {
          onProgress: (progress) =>
            config?.onProgress?.(progress, task, request),
        });
      }

      // React Query awaits a promise returned by onSuccess before onSettled, so
      // an async `success` handler holds the run open the same way here.
      await config?.success?.(result, request);
      config?.options?.onSuccess?.(result, request);
      return result;
    } catch (error) {
      config?.error?.(error, request);
      config?.options?.onError?.(error, request);
      throw error;
    } finally {
      config?.options?.onSettled?.();
    }
  };

  return {
    watch: (task, variables) => {
      void run(variables, task).catch(() => undefined);
    },
    mutate: (variables) => {
      void run(variables).catch(() => undefined);
    },
    mutateAsync: (variables) => run(variables),
  };
}
