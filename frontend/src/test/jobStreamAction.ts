// Imported from the leaf modules rather than "@/api": test files that mock the
// "@/api" barrel pull this helper in from inside that mock factory, and going
// through the barrel would re-enter it.
import type { JobError, JobSnapshot } from "@/api/generated/linuxio-types";
import { isTerminalJobState } from "@/api/job-state";
import { LinuxIOError } from "@/api/linuxio-core";

/**
 * Test double for `useJobStreamAction`, mirroring the real lifecycle in
 * `waitForJobStreamAction` + `useActionMutation`:
 *
 *   onMutate -> submit -> onJobStart -> (terminal snapshot short-circuit)
 *   -> openStream -> onOpen -> progress frames -> success/error -> onSettled
 *
 * Hand-rolled mocks kept drifting from that order — most importantly by never
 * firing `onSettled` (which owns per-run ref cleanup) and by skipping
 * `onJobStart` on `attach` (which is what gives a recovered job its job id, and
 * therefore a working cancel). Tests drive the stream through `runStream` and
 * assert against the same callback order production code sees.
 */

interface JobSnapshotLike {
  error?: JobError;
  id: string;
  progress?: unknown;
  result?: unknown;
  state?: JobSnapshot["state"];
}

interface StreamHandlers {
  onProgress: (progress: unknown) => void;
}

export interface JobStreamActionMockConfig {
  error?: (error: unknown, variables: unknown) => unknown;
  onJobStart?: (job: unknown, variables: unknown) => void;
  onOpen?: (stream: unknown, job: unknown, variables: unknown) => void;
  onProgress?: (progress: unknown, job: unknown, variables: unknown) => void;
  openErrorMessage?: string;
  options?: {
    onError?: (error: unknown, variables: unknown) => void;
    onMutate?: (variables: unknown) => void;
    onSettled?: () => void;
    onSuccess?: (result: unknown, variables: unknown) => void;
  };
  success?: (result: unknown, variables: unknown) => unknown;
}

export interface JobStreamActionMockDeps {
  /** Mirrors `openJobAttachStream`; a falsy return simulates an open failure. */
  openStream: (jobId: string) => unknown;
  /** Mirrors `waitForStreamResult`: resolve with the result, reject to fail. */
  runStream: (stream: unknown, handlers: StreamHandlers) => Promise<unknown>;
  /** Mirrors the create call; resolves the job snapshot. */
  submit: (request: unknown) => Promise<JobSnapshotLike>;
}

export interface JobStreamActionMock {
  attach: (job: JobSnapshotLike, variables: unknown) => void;
  mutate: (variables: unknown) => void;
  mutateAsync: (variables: unknown) => Promise<unknown>;
}

export function createJobStreamActionMock(
  { openStream, runStream, submit }: JobStreamActionMockDeps,
  config?: JobStreamActionMockConfig,
): JobStreamActionMock {
  const run = async (request: unknown, attachJob?: JobSnapshotLike) => {
    config?.options?.onMutate?.(request);
    try {
      const job = attachJob ?? (await submit(request));
      config?.onJobStart?.(job, request);

      let result: unknown;
      if (job.state !== undefined && isTerminalJobState(job.state)) {
        if (job.progress !== undefined && job.progress !== null) {
          config?.onProgress?.(job.progress, job, request);
        }
        if (job.state !== "completed") {
          throw new LinuxIOError(
            job.error?.message ?? "Job failed",
            job.error?.code,
          );
        }
        result = job.result;
      } else {
        const stream = openStream(job.id);
        if (!stream) {
          throw new LinuxIOError(
            config?.openErrorMessage ?? "Failed to attach job stream",
            "stream_unavailable",
          );
        }
        config?.onOpen?.(stream, job, request);
        result = await runStream(stream, {
          onProgress: (progress) =>
            config?.onProgress?.(progress, job, request),
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
    attach: (job, variables) => {
      void run(variables, job).catch(() => undefined);
    },
    mutate: (variables) => {
      void run(variables).catch(() => undefined);
    },
    mutateAsync: (variables) => run(variables),
  };
}
