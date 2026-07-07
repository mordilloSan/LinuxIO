/**
 * LinuxIO API Usage Guidelines:
 *
 * 1. TYPE-SAFE API (preferred for built-in handlers):
 *    linuxio.docker.start_container.useJobAction({ invalidates, success, error })
 *    linuxio.docker.compose.useJobStreamAction({ onProgress })
 *    linuxio.filebrowser.resource_get.useQuery()
 *
 * 2. STREAMING API (for progress tracking):
 *    openTerminalStream(), openJobDataStream(), etc. from @/api
 */

import {
  type QueryKey,
  useMutation,
  type UseMutationResult,
  type UseMutationOptions,
  useQuery,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { toast } from "sonner";

import type {
  CommandInput,
  CommandName,
  CommandRequest,
  CommandResult,
  JobSnapshot,
  HandlerName,
} from "./generated/linuxio-types";

import { getRouteMode, routeName } from "./generated/route-metadata";
import {
  isJobSnapshot,
  isTerminalJobState,
  jobSnapshotResult,
  markJobLocallyHandled,
  unmarkJobLocallyHandled,
  waitForJobCompletion,
} from "./jobs";
import { openJobAttachStream, useIsUpdating, useStreamMux } from "./linuxio";
import * as core from "./linuxio-core";
import { LinuxIOError } from "./linuxio-core";
import type { ProgressFrame, ResultFrame, Stream } from "./StreamMultiplexer";
import { waitForStreamResult } from "./stream-helpers";
import { ROUTE_INVALIDATIONS } from "@/constants/routeInvalidations";
import type { ToastMeta } from "@/contexts/ToastContext";
import { getMutationErrorMessage } from "@/utils/mutations";

// Cache TTL presets for staleTime / gcTime options
export const CACHE_TTL_MS = {
  NONE: 0,
  TWO_SECONDS: 2_000,
  FIVE_SECONDS: 5_000,
  THIRTY_SECONDS: 30_000,
  ONE_MINUTE: 60_000,
  FIVE_MINUTES: 5 * 60 * 1000,
  ONE_DAY: 24 * 60 * 60 * 1000,
} as const;

const RETRYABLE_COMMAND_PREFIXES = ["get_", "list_", "validate_"] as const;
const RETRYABLE_COMMANDS = new Set([
  "control.version",
  "filebrowser.dir_size",
  "filebrowser.indexer_status",
  "filebrowser.resource_get",
  "filebrowser.resource_stat",
  "filebrowser.search",
  "filebrowser.subfolders",
  "filebrowser.users_groups",
  "wireguard.peer_config_download",
  "wireguard.peer_qrcode",
]);

function getRetryPolicy(
  handler: string,
  command: string,
): core.RequestOptions["retryPolicy"] {
  const route = routeName(handler, command);
  if (
    RETRYABLE_COMMAND_PREFIXES.some((prefix) => command.startsWith(prefix)) ||
    RETRYABLE_COMMANDS.has(route)
  ) {
    return "connection_closed";
  }
  return "none";
}

export type RequestShape =
  { kind: "none" } | { kind: "object" } | { kind: "field"; field: string };

// ============================================================================
// Type-Safe API
// ============================================================================

/**
 * Query options type
 */
type QueryOptions<TResult, TData = TResult> = Omit<
  UseQueryOptions<TResult, LinuxIOError, TData>,
  "queryKey" | "queryFn"
>;

type MutationOptions<TRequest, TResult> = Omit<
  UseMutationOptions<TResult, LinuxIOError, TRequest>,
  "mutationFn"
>;

/**
 * Declarative config for `useJobAction`.
 *
 * Unlike `useMutation`, job actions resolve with the job's unwrapped result
 * (`JobSnapshot.result`), so `success` and `invalidates` callbacks receive the
 * actual `TResult` — no manual `jobSnapshotResult()` needed.
 */
export interface JobActionConfig<TRequest, TResult> {
  /**
   * Query keys to invalidate after success (static, or derived from
   * result/variables). Defaults to the route's `ROUTE_INVALIDATIONS` manifest
   * entry; pass `[]` to opt out, or a value to override the manifest.
   */
  invalidates?:
    QueryKey[] | ((result: TResult, variables: TRequest) => QueryKey[]);
  /** Success toast message, or a callback for custom success handling. */
  success?: string | ((result: TResult, variables: TRequest) => void);
  /**
   * Error toast fallback message — the server error message still wins
   * (`getMutationErrorMessage` semantics) — or a callback for custom handling.
   */
  error?: string | ((error: LinuxIOError, variables: TRequest) => void);
  /** Toast meta ({ href, label }) attached to string success/error toasts. */
  toast?: ToastMeta;
  /** Escape hatch: raw React Query options; its handlers run after the config ones. */
  options?: MutationOptions<TRequest, TResult>;
}

type StreamSignal<TRequest> =
  AbortSignal | ((variables: TRequest) => AbortSignal | undefined);

/**
 * Declarative config for job routes where the UI needs live progress frames.
 *
 * Like `useJobAction`, this starts a job route through React Query and applies
 * invalidation/toast handling on completion. Unlike `useJobAction`, it attaches
 * to the job stream directly so callers can render each progress frame.
 */
export interface JobStreamActionConfig<
  TRequest,
  TResult,
  TProgress = ProgressFrame,
> extends JobActionConfig<TRequest, TResult> {
  /**
   * By default a locally awaited job is marked handled so the recovered-jobs
   * stream skips its completion toasts/invalidations. Pass `false` when the
   * global handler should keep ownership of completion — e.g. progress is
   * rendered locally but the toast must still fire if the caller unmounts.
   */
  markHandled?: boolean;
  /** Abort signal for this run, or a callback that derives one from variables. */
  signal?: StreamSignal<TRequest>;
  /** Action to perform on abort signal. Defaults to aborting the stream. */
  closeOnAbort?: "abort" | "close" | "none";
  /** Error text when the job stream cannot be opened. */
  openErrorMessage?: string;
  /** Error code when the job stream cannot be opened. */
  openErrorCode?: string;
  /** Error text when the stream closes before a result frame. */
  closeMessage?: string;
  /** Called once the job snapshot is created and before attaching. */
  onJobStart?: (job: JobSnapshot, variables: TRequest) => void;
  /** Called once the attach stream is open. */
  onOpen?: (stream: Stream, job: JobSnapshot, variables: TRequest) => void;
  /** Called for each progress frame emitted by the job stream. */
  onProgress?: (
    progress: TProgress,
    job: JobSnapshot,
    variables: TRequest,
  ) => void;
  /** Called if the stream closes before a terminal result frame. */
  onClose?: (job: JobSnapshot, variables: TRequest) => void;
  /** Optional result mapper for the final stream result frame. */
  mapResult?: (
    data: unknown,
    frame: ResultFrame,
    job: JobSnapshot,
    variables: TRequest,
  ) => TResult;
}

type QueryOptionsArgs<
  TInput extends readonly unknown[],
  TResult,
  TData = TResult,
> = TInput extends readonly []
  ? [options?: QueryOptions<TResult, TData>]
  : [...input: TInput, options?: QueryOptions<TResult, TData>];

/**
 * Command endpoint interface
 */
export interface CommandEndpoint<
  TInput extends readonly unknown[],
  TRequest,
  TResult,
> {
  /**
   * Framework-agnostic call (Promise-based) using the same generated request
   * shape and cache key scheme as the React Query hooks.
   */
  (...args: TInput): Promise<TResult>;

  /** Deterministic React Query key for this command */
  queryKey: (...args: TInput) => QueryKey;

  /**
   * React Query options for `queryClient.fetchQuery/ensureQueryData`
   * and non-hook integration points.
   */
  queryOptions: <TData = TResult>(
    ...params: QueryOptionsArgs<TInput, TResult, TData>
  ) => UseQueryOptions<TResult, LinuxIOError, TData>;

  /**
   * Mutation hook for job routes: awaits job completion,
   * unwraps the job result, and handles invalidation + toasts declaratively.
   *
   * @example
   * const { mutate } = linuxio.docker.start_container.useJobAction({
   *   invalidates: [linuxio.docker.list_containers.queryKey()],
   *   success: "Container started",
   *   error: "Failed to start container",
   *   toast: { href: "/docker", label: "Open Docker" },
   * });
   * mutate({ containerId });
   */
  useJobAction: (
    config?: JobActionConfig<TRequest, TResult>,
  ) => UseMutationResult<TResult, LinuxIOError, TRequest>;

  /**
   * Higher-level mutation hook for job routes with live progress streaming.
   *
   * @example
   * const compose = linuxio.docker.compose.useJobStreamAction({
   *   onProgress: (frame) => setProgress(frame),
   *   invalidates: [linuxio.docker.list_compose_projects.queryKey()],
   * });
   * compose.mutate({ action: "up", projectName });
   */
  useJobStreamAction: <TStreamResult = TResult, TProgress = ProgressFrame>(
    config?: JobStreamActionConfig<TRequest, TStreamResult, TProgress>,
  ) => UseMutationResult<TStreamResult, LinuxIOError, TRequest>;

  /**
   * React Query hook for fetching data
   *
   * @example
   * // No arguments
   * useQuery()
   *
   * @example
   * // Single-field generated request with options
   * useQuery(unitName, { staleTime: 60000 })
   */
  useQuery: <TData = TResult>(
    ...params: QueryOptionsArgs<TInput, TResult, TData>
  ) => ReturnType<typeof useQuery<TResult, LinuxIOError, TData>>;
}

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requestForWire(requestShape: RequestShape, request: unknown): unknown {
  switch (requestShape.kind) {
    case "none":
      return {};
    case "field":
      if (isObjectRecord(request) && requestShape.field in request) {
        return request;
      }
      return { [requestShape.field]: request };
    case "object":
      return request ?? {};
  }
}

function queryRequestAndOptions<TOptions>(
  requestShape: RequestShape,
  params: unknown[],
): { request: unknown; options: TOptions | undefined } {
  if (requestShape.kind === "none") {
    return { request: undefined, options: params[0] as TOptions | undefined };
  }
  return {
    request: params[0],
    options: params[1] as TOptions | undefined,
  };
}

function buildQueryOptions<TResult, TData = TResult>(
  handler: string,
  command: string,
  requestShape: RequestShape,
  request: unknown,
  options?: QueryOptions<TResult, TData>,
): UseQueryOptions<TResult, LinuxIOError, TData> {
  const route = routeName(handler, command);
  const mode = getRouteMode(route);
  if (mode && mode !== "query") {
    throw new LinuxIOError(
      `Route ${route} is ${mode}, not query`,
      "invalid_route_mode",
    );
  }
  const wireRequest = requestForWire(requestShape, request);

  return {
    queryKey:
      requestShape.kind === "none"
        ? ["linuxio", handler, command]
        : ["linuxio", handler, command, wireRequest],
    queryFn: () =>
      core.request<TResult>(handler, command, wireRequest, {
        retryPolicy: getRetryPolicy(handler, command),
      }),
    ...(options ?? {}),
  };
}

function resolveSignal<TRequest>(
  signal: StreamSignal<TRequest> | undefined,
  variables: TRequest,
): AbortSignal | undefined {
  return typeof signal === "function" ? signal(variables) : signal;
}

async function waitForJobStreamAction<
  TRequest,
  TResult,
  TProgress = ProgressFrame,
>(
  snapshot: JobSnapshot,
  variables: TRequest,
  config: JobStreamActionConfig<TRequest, TResult, TProgress> | undefined,
  signal: AbortSignal | undefined,
): Promise<TResult> {
  config?.onJobStart?.(snapshot, variables);

  if (isTerminalJobState(snapshot.state)) {
    if (snapshot.progress !== undefined && snapshot.progress !== null) {
      config?.onProgress?.(snapshot.progress as TProgress, snapshot, variables);
    }
    if (snapshot.state === "completed") {
      return jobSnapshotResult<TResult>(snapshot);
    }
    throw new LinuxIOError(
      snapshot.error?.message ?? "Job failed",
      snapshot.error?.code,
    );
  }

  const attach = openJobAttachStream(snapshot.id);
  if (!attach) {
    throw new LinuxIOError(
      config?.openErrorMessage ?? "Failed to attach job stream",
      config?.openErrorCode ?? "stream_unavailable",
    );
  }

  const markHandled = config?.markHandled !== false;
  if (markHandled) {
    markJobLocallyHandled(snapshot.id);
  }
  config?.onOpen?.(attach, snapshot, variables);

  const mapResult = config?.mapResult;
  try {
    return await waitForStreamResult<TResult, TProgress>(attach, {
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
  } finally {
    if (markHandled) {
      unmarkJobLocallyHandled(snapshot.id);
    }
  }
}

/**
 * Create a command endpoint factory
 */
export function createEndpoint<TResult>(
  handler: string,
  command: string,
  requestShape: RequestShape,
): CommandEndpoint<[] | [unknown], unknown, TResult> {
  const retryPolicy = getRetryPolicy(handler, command);
  const queryKey = (...rawArgs: [] | [unknown]): QueryKey => {
    const request = rawArgs[0];
    const wireRequest = requestForWire(requestShape, request);
    return requestShape.kind === "none"
      ? (["linuxio", handler, command] as const)
      : (["linuxio", handler, command, wireRequest] as const);
  };

  const execute = (...rawArgs: [] | [unknown]): Promise<TResult> => {
    const request = rawArgs[0];
    return core.request<TResult>(
      handler,
      command,
      requestForWire(requestShape, request),
      { retryPolicy },
    );
  };

  const queryOptions = <TData = TResult>(
    ...params: unknown[]
  ): UseQueryOptions<TResult, LinuxIOError, TData> => {
    const { request, options } = queryRequestAndOptions<
      QueryOptions<TResult, TData>
    >(requestShape, params);
    return buildQueryOptions<TResult, TData>(
      handler,
      command,
      requestShape,
      request,
      options,
    );
  };

  const endpoint = ((...rawArgs: [] | [unknown]) =>
    execute(...rawArgs)) as CommandEndpoint<[] | [unknown], unknown, TResult>;

  endpoint.queryKey = queryKey;
  endpoint.queryOptions = queryOptions;
  endpoint.useQuery = (<TData = TResult>(
    ...params: unknown[]
  ): ReturnType<typeof useQuery<TResult, LinuxIOError, TData>> => {
    const { isOpen } = useStreamMux();
    const isUpdating = useIsUpdating();

    const baseOptions = queryOptions<TData>(...params);
    return useQuery<TResult, LinuxIOError, TData>({
      ...baseOptions,
      enabled: isOpen && !isUpdating && (baseOptions.enabled ?? true) === true,
    });
  }) as CommandEndpoint<[] | [unknown], unknown, TResult>["useQuery"];

  endpoint.useJobAction = (config?: JobActionConfig<unknown, TResult>) => {
    const route = routeName(handler, command);
    const mode = getRouteMode(route);
    if (mode && mode !== "job") {
      throw new LinuxIOError(
        `Route ${route} is ${mode}, not mutation/job`,
        "invalid_route_mode",
      );
    }

    const queryClient = useQueryClient();
    const {
      invalidates,
      success,
      error,
      toast: toastMeta,
      options,
    } = config ?? {};
    const toastOpts = toastMeta ? { meta: toastMeta } : undefined;

    return useMutation<TResult, LinuxIOError, unknown>({
      mutationFn: async (request: unknown) => {
        const result = await core.request<TResult>(
          handler,
          command,
          requestForWire(requestShape, request),
          { retryPolicy },
        );
        if (isJobSnapshot(result)) {
          return jobSnapshotResult<TResult>(await waitForJobCompletion(result));
        }
        return result;
      },
      ...options,
      onSuccess: (result, variables, onMutateResult, context) => {
        const keys =
          typeof invalidates === "function"
            ? invalidates(result, variables)
            : (invalidates ?? ROUTE_INVALIDATIONS[route] ?? []);
        for (const queryKey of keys) {
          void queryClient.invalidateQueries({ queryKey });
        }
        if (typeof success === "function") {
          success(result, variables);
        } else if (success !== undefined) {
          toast.success(success, toastOpts);
        }
        options?.onSuccess?.(result, variables, onMutateResult, context);
      },
      onError: (err, variables, onMutateResult, context) => {
        if (typeof error === "function") {
          error(err, variables);
        } else if (error !== undefined) {
          toast.error(getMutationErrorMessage(err, error), toastOpts);
        }
        options?.onError?.(err, variables, onMutateResult, context);
      },
    });
  };

  endpoint.useJobStreamAction = (<
    TStreamResult = TResult,
    TProgress = ProgressFrame,
  >(
    config?: JobStreamActionConfig<unknown, TStreamResult, TProgress>,
  ) => {
    const route = routeName(handler, command);
    const mode = getRouteMode(route);
    if (mode && mode !== "job") {
      throw new LinuxIOError(
        `Route ${route} is ${mode}, not mutation/job`,
        "invalid_route_mode",
      );
    }

    const queryClient = useQueryClient();
    const {
      invalidates,
      success,
      error,
      toast: toastMeta,
      options,
    } = config ?? {};
    const toastOpts = toastMeta ? { meta: toastMeta } : undefined;

    return useMutation<TStreamResult, LinuxIOError, unknown>({
      mutationFn: async (request: unknown) => {
        const signal = resolveSignal(config?.signal, request);
        const result = await core.request<TResult>(
          handler,
          command,
          requestForWire(requestShape, request),
          { retryPolicy },
        );
        if (isJobSnapshot(result)) {
          return waitForJobStreamAction<unknown, TStreamResult, TProgress>(
            result,
            request,
            config,
            signal,
          );
        }
        return result as unknown as TStreamResult;
      },
      ...options,
      onSuccess: (result, variables, onMutateResult, context) => {
        const keys =
          typeof invalidates === "function"
            ? invalidates(result, variables)
            : (invalidates ?? ROUTE_INVALIDATIONS[route] ?? []);
        for (const queryKey of keys) {
          void queryClient.invalidateQueries({ queryKey });
        }
        if (typeof success === "function") {
          success(result, variables);
        } else if (success !== undefined) {
          toast.success(success, toastOpts);
        }
        options?.onSuccess?.(result, variables, onMutateResult, context);
      },
      onError: (err, variables, onMutateResult, context) => {
        if (typeof error === "function") {
          error(err, variables);
        } else if (error !== undefined) {
          toast.error(getMutationErrorMessage(err, error), toastOpts);
        }
        options?.onError?.(err, variables, onMutateResult, context);
      },
    });
  }) as CommandEndpoint<[] | [unknown], unknown, TResult>["useJobStreamAction"];

  return endpoint;
}

// ============================================================================
// Handler Namespace Types
// ============================================================================

/**
 * Maps a handler's commands to their endpoints
 */
export type HandlerEndpoints<H extends HandlerName> = {
  [C in CommandName<H>]: CommandEndpoint<
    CommandInput<H, C>,
    CommandRequest<H, C>,
    CommandResult<H, C>
  >;
};

/**
 * Full typed API structure
 */
export type TypedAPI = {
  [H in HandlerName]: HandlerEndpoints<H>;
};

// Re-export types for convenience
export type {
  LinuxIOSchema,
  HandlerName,
  CommandName,
  CommandInput,
  CommandRequest,
  CommandResult,
} from "./generated/linuxio-types";
