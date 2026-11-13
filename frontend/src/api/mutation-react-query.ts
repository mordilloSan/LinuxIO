/**
 * TanStack Query integration for Mutation routes: one stream carries the job's
 * snapshot, progress and result, with no client timeout. Transport recovery
 * (resend before the snapshot, reattach after it) lives in `linuxio-core.ts`.
 */

import { type QueryKey, type UseMutationResult } from "@tanstack/react-query";
import { useRef } from "react";

import { type ActionConfig, useActionMutation } from "./call-react-query";
import type { LinuxIOError } from "./errors";
import type { TaskSnapshot } from "./generated/linuxio-types";
import { routeName } from "./generated/route-metadata";
import * as core from "./linuxio-core";
import {
  endpointQueryKey,
  requestForWire,
  type RequestShape,
} from "./query-keys";
import type { ProgressFrame } from "./StreamMultiplexer";

/** Declarative config for a Mutation: ActionConfig plus job lifecycle hooks. */
export interface MutationConfig<
  TRequest,
  TResult,
  TProgress,
> extends ActionConfig<TRequest, TResult> {
  /** Called once with the job's snapshot, before any progress. */
  onStart?: (snapshot: TaskSnapshot, variables: TRequest) => void;
  /** Called for each progress frame the job emits. */
  onProgress?: (
    progress: TProgress,
    snapshot: TaskSnapshot,
    variables: TRequest,
  ) => void;
  /** Stops following the job when aborted, per `closeOnAbort`. */
  signal?: AbortSignal | ((variables: TRequest) => AbortSignal | undefined);
  /** On abort: "abort" (default) cancels the job, "close" detaches from it. */
  closeOnAbort?: "abort" | "close";
}

/** Public endpoint surface for Mutation routes. */
export interface MutationEndpoint<
  TInput extends readonly unknown[],
  TRequest,
  TResult,
  TProgress,
> {
  (...args: TInput): Promise<TResult>;
  /** Start a job and resolve with its first snapshot, without following it. */
  start(...args: TInput): Promise<TaskSnapshot>;
  useMutation(
    config?: MutationConfig<TRequest, TResult, TProgress>,
  ): UseMutationResult<TResult, LinuxIOError, TRequest> & {
    /** Adopt an already-running job of this route by id. */
    watch: (taskId: string, variables: TRequest) => void;
  };
  queryKey: (...args: TInput) => QueryKey;
}

/** Create an endpoint for one Mutation route. */
export function defineMutation<TResult, TProgress = ProgressFrame>(
  handler: string,
  command: string,
  requestShape: RequestShape,
): MutationEndpoint<[] | [unknown], unknown, TResult, TProgress> {
  const endpoint = ((...rawArgs: [] | [unknown]) =>
    core.mutationRequest<TResult, TProgress>(
      handler,
      command,
      requestForWire(requestShape, rawArgs[0]),
    )) as MutationEndpoint<[] | [unknown], unknown, TResult, TProgress>;

  endpoint.queryKey = (...rawArgs: [] | [unknown]) =>
    endpointQueryKey(handler, command, requestShape, rawArgs[0]);
  endpoint.start = (...rawArgs: [] | [unknown]) =>
    core.startJob(handler, command, requestForWire(requestShape, rawArgs[0]));

  endpoint.useMutation = (config) => {
    const adoptedTaskRef = useRef<string | null>(null);
    const mutation = useActionMutation<TResult>(
      routeName(handler, command),
      (request: unknown) => {
        const adoptedTaskId = adoptedTaskRef.current;
        adoptedTaskRef.current = null;
        let task: TaskSnapshot | undefined;
        const handlers: core.MutationHandlers<TProgress> = {
          onSnapshot: (snapshot) => {
            task = snapshot;
            config?.onStart?.(snapshot, request);
          },
          onProgress: (progress) => {
            if (task) config?.onProgress?.(progress, task, request);
          },
          signal:
            typeof config?.signal === "function"
              ? config.signal(request)
              : config?.signal,
          closeOnAbort: config?.closeOnAbort,
        };
        return adoptedTaskId === null
          ? core.mutationRequest<TResult, TProgress>(
              handler,
              command,
              requestForWire(requestShape, request),
              handlers,
            )
          : core.watchMutation<TResult, TProgress>(adoptedTaskId, handlers);
      },
      config,
    );
    const watch = (taskId: string, variables: unknown) => {
      adoptedTaskRef.current = taskId;
      mutation.mutate(variables);
    };
    return { ...mutation, watch };
  };

  return endpoint;
}
