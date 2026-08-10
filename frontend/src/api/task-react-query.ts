/**
 * TanStack Query integration for tracked Task mutations.
 *
 * Cached Call reads and bounded actions live in `call-react-query.ts`. This
 * module owns Task completion, watching, and progress-stream behavior.
 */

import { type QueryKey, type UseMutationResult } from "@tanstack/react-query";
import { useCallback, useRef } from "react";

import { type ActionConfig, useActionMutation } from "./call-react-query";
import { getRetryPolicy } from "./calls";
import type { TaskSnapshot } from "./generated/linuxio-types";
import { getRouteMode, routeName } from "./generated/route-metadata";
import { openTaskWatchStream } from "./linuxio";
import * as core from "./linuxio-core";
import { LinuxIOError } from "./linuxio-core";
import {
  endpointQueryKey,
  requestForWire,
  type RequestShape,
} from "./query-keys";
import { waitForStreamResult } from "./stream-helpers";
import type { ProgressFrame, ResultFrame, Stream } from "./StreamMultiplexer";
import {
  isTerminalTaskState,
  taskSnapshotResult,
  waitForTaskCompletion,
} from "./tasks";

type StreamSignal<TRequest> =
  | AbortSignal
  | ((variables: TRequest) => AbortSignal | undefined);

/** Declarative config for Task routes with live progress frames. */
export interface TaskStreamActionConfig<
  TRequest,
  TResult,
  TProgress = ProgressFrame,
> extends ActionConfig<TRequest, TResult> {
  /** Abort signal for this run, or a callback that derives one from variables. */
  signal?: StreamSignal<TRequest>;
  /** Action to perform on abort signal. Defaults to aborting the watch stream. */
  closeOnAbort?: "abort" | "close" | "none";
  /** Error text when the Task watch stream cannot be opened. */
  openErrorMessage?: string;
  /** Error code when the Task watch stream cannot be opened. */
  openErrorCode?: string;
  /** Error text when the watch stream closes before a result frame. */
  closeMessage?: string;
  /** Called once the Task snapshot is created and before watching. */
  onTaskStart?: (task: TaskSnapshot, variables: TRequest) => void;
  /** Called once the watch stream is open. */
  onOpen?: (stream: Stream, task: TaskSnapshot, variables: TRequest) => void;
  /** Called for each progress frame emitted by the Task. */
  onProgress?: (
    progress: TProgress,
    task: TaskSnapshot,
    variables: TRequest,
  ) => void;
  /** Called if the watch closes before a terminal result frame. */
  onClose?: (task: TaskSnapshot, variables: TRequest) => void;
  /** Optional result mapper for the final watch result frame. */
  mapResult?: (
    data: unknown,
    frame: ResultFrame,
    task: TaskSnapshot,
    variables: TRequest,
  ) => TResult;
}

/** A Task mutation plus adoption of an already-running Task. */
export type TaskStreamActionResult<TRequest, TResult> = UseMutationResult<
  TResult,
  LinuxIOError,
  TRequest
> & {
  watch: (task: TaskSnapshot, variables: TRequest) => void;
};

interface BaseTaskEndpoint<TInput extends readonly unknown[]> {
  (...args: TInput): Promise<TaskSnapshot>;
  queryKey: (...args: TInput) => QueryKey;
}

interface TaskEndpointCapabilities<
  TRequest,
  TResult,
  TProgress = ProgressFrame,
> {
  useTaskAction: (
    config?: ActionConfig<TRequest, TResult>,
  ) => UseMutationResult<TResult, LinuxIOError, TRequest>;
  useTaskStreamAction: <TStreamResult = TResult, TStreamProgress = TProgress>(
    config?: TaskStreamActionConfig<TRequest, TStreamResult, TStreamProgress>,
  ) => TaskStreamActionResult<TRequest, TStreamResult>;
}

/** Public endpoint surface for progress/recovery Task routes. */
export type TaskEndpoint<
  TInput extends readonly unknown[],
  TRequest,
  TResult,
  TProgress = ProgressFrame,
> = BaseTaskEndpoint<TInput> &
  TaskEndpointCapabilities<TRequest, TResult, TProgress>;

function assertTaskRoute(route: string): void {
  const mode = getRouteMode(route);
  if (mode && mode !== "task") {
    throw new LinuxIOError(
      `Route ${route} is ${mode}, not mutation/task`,
      "invalid_route_mode",
    );
  }
}

function resolveSignal<TRequest>(
  signal: StreamSignal<TRequest> | undefined,
  variables: TRequest,
): AbortSignal | undefined {
  return typeof signal === "function" ? signal(variables) : signal;
}

async function waitForTaskStreamAction<
  TRequest,
  TResult,
  TProgress = ProgressFrame,
>(
  snapshot: TaskSnapshot,
  variables: TRequest,
  config: TaskStreamActionConfig<TRequest, TResult, TProgress> | undefined,
  signal: AbortSignal | undefined,
): Promise<TResult> {
  config?.onTaskStart?.(snapshot, variables);

  if (isTerminalTaskState(snapshot.state)) {
    if (snapshot.progress !== undefined && snapshot.progress !== null) {
      config?.onProgress?.(snapshot.progress as TProgress, snapshot, variables);
    }
    if (snapshot.state === "completed") {
      return taskSnapshotResult<TResult>(snapshot);
    }
    throw new LinuxIOError(
      snapshot.error?.message ?? "Task failed",
      snapshot.error?.code,
    );
  }

  const watch = openTaskWatchStream(snapshot.id);
  if (!watch) {
    throw new LinuxIOError(
      config?.openErrorMessage ?? "Failed to watch task",
      config?.openErrorCode ?? "stream_unavailable",
    );
  }

  config?.onOpen?.(watch, snapshot, variables);

  const mapResult = config?.mapResult;
  return waitForStreamResult<TResult, TProgress>(watch, {
    closeMessage: config?.closeMessage,
    closeOnAbort: config?.closeOnAbort,
    mapResult: mapResult
      ? (data, frame) => mapResult(data, frame, snapshot, variables)
      : undefined,
    onClose: () => config?.onClose?.(snapshot, variables),
    onProgress: (progress) =>
      config?.onProgress?.(progress, snapshot, variables),
    signal,
  });
}

/** Create an endpoint for one Task route. */
export function createTaskEndpoint<TResult>(
  handler: string,
  command: string,
  requestShape: RequestShape,
): TaskEndpoint<[] | [unknown], unknown, TResult> {
  const retryPolicy = getRetryPolicy(handler, command);
  const queryKey = (...rawArgs: [] | [unknown]): QueryKey =>
    endpointQueryKey(handler, command, requestShape, rawArgs[0]);
  const execute = (...rawArgs: [] | [unknown]): Promise<TaskSnapshot> =>
    core.request<TaskSnapshot>(
      handler,
      command,
      requestForWire(requestShape, rawArgs[0]),
      { retryPolicy },
    );

  const endpoint = ((...rawArgs: [] | [unknown]) =>
    execute(...rawArgs)) as TaskEndpoint<[] | [unknown], unknown, TResult>;

  endpoint.queryKey = queryKey;

  endpoint.useTaskAction = (config?: ActionConfig<unknown, TResult>) => {
    const route = routeName(handler, command);
    assertTaskRoute(route);
    return useActionMutation<TResult>(
      route,
      async (request: unknown) => {
        const result = await execute(request);
        return taskSnapshotResult<TResult>(await waitForTaskCompletion(result));
      },
      config,
    );
  };

  endpoint.useTaskStreamAction = (<
    TStreamResult = TResult,
    TProgress = ProgressFrame,
  >(
    config?: TaskStreamActionConfig<unknown, TStreamResult, TProgress>,
  ) => {
    const route = routeName(handler, command);
    assertTaskRoute(route);
    const watchedTaskRef = useRef<TaskSnapshot | null>(null);
    const mutation = useActionMutation<TStreamResult>(
      route,
      async (request: unknown) => {
        const signal = resolveSignal(config?.signal, request);
        const watchedTask = watchedTaskRef.current;
        watchedTaskRef.current = null;
        const result = watchedTask ?? (await execute(request));
        return waitForTaskStreamAction<unknown, TStreamResult, TProgress>(
          result,
          request,
          config,
          signal,
        );
      },
      config,
    );
    const { mutate } = mutation;
    const watch = useCallback(
      (task: TaskSnapshot, variables: unknown) => {
        watchedTaskRef.current = task;
        mutate(variables);
      },
      [mutate],
    );
    return { ...mutation, watch };
  }) as TaskEndpoint<[] | [unknown], unknown, TResult>["useTaskStreamAction"];

  return endpoint;
}
