/**
 * LinuxIO API Usage Guidelines:
 *
 * 1. TYPE-SAFE API (preferred for built-in handlers):
 *    Render-driven reads:    useQuery(linuxio.filebrowser.resource_get.queryOptions())
 *                            useQueries({ queries: paths.map((path) =>
 *                              linuxio.filebrowser.dir_size.queryOptions(path)) })
 *    Event-driven commands:  linuxio.docker.validate_compose.useAction({ error })
 *    Direct action routes:   linuxio.docker.start_container.useAction({ invalidates, success, error })
 *    Progress job routes:    linuxio.docker.compose.useJobStreamAction({ onProgress })
 *    Loader/effect reads:    linuxio.jobs.list.useFetcher()
 *    Cache surgery:          linuxio.virt.list.useCache().set(updater)
 *
 * 2. STREAMING API (for progress tracking):
 *    openTerminalStream(), openJobDataStream(), etc. from @/api
 */

import {
  queryOptions as createQueryOptions,
  type QueryFunction,
  type QueryKey,
  useMutation,
  type UseMutationResult,
  type UseMutationOptions,
  useQueryClient,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { useCallback, useMemo, useRef } from "react";
import { toast } from "sonner";

import type { ToastMeta } from "@/types/navigation";
import { getMutationErrorMessage } from "@/utils/mutations";

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
import { openJobAttachStream } from "./linuxio";
import * as core from "./linuxio-core";
import { LinuxIOError } from "./linuxio-core";
import { OPERATION_QUERY_INVALIDATIONS } from "./operation-query-invalidations";
import {
  endpointQueryKey,
  endpointQueryPrefix,
  requestForWire,
  type RequestShape,
} from "./query-keys";
import { waitForStreamResult } from "./stream-helpers";
import type { ProgressFrame, ResultFrame, Stream } from "./StreamMultiplexer";

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

export type { RequestShape } from "./query-keys";

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

type EndpointQueryOptions<TResult, TData = TResult> = UseQueryOptions<
  TResult,
  LinuxIOError,
  TData
> & {
  queryFn: QueryFunction<TResult, QueryKey>;
  queryKey: QueryKey;
};

type MutationOptions<TRequest, TResult> = Omit<
  UseMutationOptions<TResult, LinuxIOError, TRequest>,
  "mutationFn"
>;

/**
 * Declarative config shared by `useAction` and `useJobAction`.
 *
 * Unlike raw `useMutation`, job actions resolve with the job's unwrapped
 * result (`JobSnapshot.result`), so `success` and `invalidates` callbacks
 * receive the actual `TResult` — no manual `jobSnapshotResult()` needed.
 */
export interface ActionConfig<TRequest, TResult> {
  /**
   * Query keys to invalidate after success (static, or derived from
   * result/variables). Defaults to the route's `OPERATION_QUERY_INVALIDATIONS`
   * manifest
   * entry; pass `[]` to opt out, or a value to override the manifest.
   */
  invalidates?:
    | QueryKey[]
    | ((result: TResult, variables: TRequest) => QueryKey[]);
  /** Success toast message, or a callback for custom success handling. */
  success?: string | ((result: TResult, variables: TRequest) => void);
  /**
   * Extract a warning from a successful result (e.g. `result.warning`). When
   * it returns a non-empty string, a warning toast fires and replaces the
   * string-form success toast; invalidation and a callback-form `success`
   * still run — a warning is still a success.
   */
  warning?: (result: TResult, variables: TRequest) => string | null | undefined;
  /**
   * Error toast fallback message — the server error message still wins
   * (`getMutationErrorMessage` semantics) — or a callback for custom handling.
   */
  error?: string | ((error: LinuxIOError, variables: TRequest) => void);
  /** Typed route target attached to string success/error toasts. */
  toast?: ToastMeta;
  /** Escape hatch: raw React Query options; its handlers run after the config ones. */
  options?: MutationOptions<TRequest, TResult>;
}

type StreamSignal<TRequest> =
  | AbortSignal
  | ((variables: TRequest) => AbortSignal | undefined);

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
> extends ActionConfig<TRequest, TResult> {
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

/**
 * Result of `useJobStreamAction`: a normal mutation plus `attach`, which
 * adopts an already-running job (e.g. found by `useActiveJobRecovery` after a
 * page reload) into the same config lifecycle — progress frames, toasts,
 * invalidation, and pending state behave exactly as if `mutate(variables)`
 * had started the job.
 */
export type JobStreamActionResult<TRequest, TResult> = UseMutationResult<
  TResult,
  LinuxIOError,
  TRequest
> & {
  attach: (job: JobSnapshot, variables: TRequest) => void;
};

type QueryOptionsArgs<
  TInput extends readonly unknown[],
  TResult,
  TData = TResult,
> = TInput extends readonly []
  ? [options?: QueryOptions<TResult, TData>]
  : [...input: TInput, options?: QueryOptions<TResult, TData>];

/**
 * Typed cache handle for one endpoint (see `useCache`): entry-level `get`/
 * `set`, and `invalidate`/`remove`/`cancel` that target one request's entry —
 * or, called with no input, every entry of the endpoint.
 */
export interface EndpointCache<TInput extends readonly unknown[], TResult> {
  /** Cached result for one request, if present. */
  get: (...input: TInput) => TResult | undefined;
  /** Write one request's cache entry: a value, or an updater of the old one. */
  set: (
    ...args: [
      ...TInput,
      TResult | ((old: TResult | undefined) => TResult | undefined),
    ]
  ) => void;
  /** Mark entries stale and refetch the active ones. */
  invalidate: (...input: TInput | []) => Promise<void>;
  /** Drop entries from the cache. */
  remove: (...input: TInput | []) => void;
  /** Cancel in-flight fetches (before an optimistic `set`). */
  cancel: (...input: TInput | []) => Promise<void>;
}

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

  /** Shared React Query key and query function for every consumer. */
  queryOptions: <TData = TResult>(
    ...params: QueryOptionsArgs<TInput, TResult, TData>
  ) => EndpointQueryOptions<TResult, TData>;

  /**
   * Hook returning an imperative fetch through the query cache — for
   * loaders and effects that need data at call time rather than render
   * time (chart backfill, lazy tree loads, workflow pre-checks). Same
   * input shape, cache keys, and options as `useQuery`; the returned
   * function is referentially stable.
   *
   * @example
   * const fetchStat = linuxio.filebrowser.resource_stat.useFetcher();
   * const stat = await fetchStat(path, { staleTime: CACHE_TTL_MS.NONE });
   */
  useFetcher: () => (
    ...params: QueryOptionsArgs<TInput, TResult>
  ) => Promise<TResult>;

  /**
   * Hook returning a typed, referentially stable cache handle bound to this
   * endpoint's key scheme — for optimistic updates and for seeding query
   * data from an action's result, without touching the query client.
   *
   * @example
   * const vmList = linuxio.virt.list.useCache();
   * vmList.set((vms) => vms?.filter((vm) => vm.name !== deleted));
   */
  useCache: () => EndpointCache<TInput, TResult>;

  /**
   * Mutation hook for query routes used as event-driven commands
   * (validation, download generation, path resolution): imperative
   * `mutate`/`mutateAsync` with pending state and the same declarative
   * invalidation/toast config as `useJobAction`. No query caching — use
   * `useQuery(endpoint.queryOptions(...))` or `useFetcher` when the result
   * is data to display.
   *
   * @example
   * const { mutate: downloadConfig } =
   *   linuxio.wireguard.peer_config_download.useAction({
   *     success: (result) => saveBlob(result.content),
   *     error: "Failed to download config",
   *   });
   * downloadConfig({ interfaceName, peerName });
   */
  useAction: (
    config?: ActionConfig<TRequest, TResult>,
  ) => UseMutationResult<TResult, LinuxIOError, TRequest>;

  /**
   * Mutation hook for job routes: awaits job completion,
   * unwraps the job result, and handles invalidation + toasts declaratively.
   *
   * @example
   * const { mutate } = linuxio.filebrowser.resource_patch.useJobAction({
   *   success: "Resource updated",
   *   error: "Failed to update resource",
   * });
   * mutate({ action: "rename", src: "/old-name", dst: "/new-name" });
   */
  useJobAction: (
    config?: ActionConfig<TRequest, TResult>,
  ) => UseMutationResult<TResult, LinuxIOError, TRequest>;

  /**
   * Higher-level mutation hook for job routes with live progress streaming.
   * The returned mutation also exposes `attach(job, variables)` to adopt an
   * already-running job (page-reload recovery) into the same config
   * lifecycle.
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
  ) => JobStreamActionResult<TRequest, TStreamResult>;
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

function assertRouteMode(route: string, expected: "query" | "job"): void {
  const mode = getRouteMode(route);
  if (mode && mode !== expected) {
    throw new LinuxIOError(
      `Route ${route} is ${mode}, not ${expected === "job" ? "mutation/job" : "query"}`,
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

/**
 * Shared mutation lifecycle behind `useAction`, `useJobAction`, and
 * `useJobStreamAction`: runs `mutationFn` through React Query and applies the
 * declarative `ActionConfig` handling — manifest-driven invalidation,
 * success/error toasts or callbacks, and the raw-options escape hatch (its
 * handlers run after the config ones).
 */
function useActionMutation<TResult>(
  route: string,
  mutationFn: (request: unknown) => Promise<TResult>,
  config: ActionConfig<unknown, TResult> | undefined,
): UseMutationResult<TResult, LinuxIOError, unknown> {
  const queryClient = useQueryClient();
  const {
    invalidates,
    success,
    warning,
    error,
    toast: toastMeta,
    options,
  } = config ?? {};
  const toastOpts = toastMeta ? { meta: toastMeta } : undefined;

  return useMutation<TResult, LinuxIOError, unknown>({
    mutationFn,
    ...options,
    onSuccess: (result, variables, onMutateResult, context) => {
      const keys =
        typeof invalidates === "function"
          ? invalidates(result, variables)
          : (invalidates ?? OPERATION_QUERY_INVALIDATIONS[route] ?? []);
      for (const queryKey of keys) {
        void queryClient.invalidateQueries({ queryKey });
      }
      const warningMessage = warning?.(result, variables);
      if (warningMessage) {
        toast.warning(warningMessage, toastOpts);
      }
      if (typeof success === "function") {
        success(result, variables);
      } else if (success !== undefined && !warningMessage) {
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
  const queryKey = (...rawArgs: [] | [unknown]): QueryKey =>
    endpointQueryKey(handler, command, requestShape, rawArgs[0]);

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
  ): EndpointQueryOptions<TResult, TData> => {
    const { request, options } = queryRequestAndOptions<
      QueryOptions<TResult, TData>
    >(requestShape, params);
    assertRouteMode(routeName(handler, command), "query");
    const input = (requestShape.kind === "none" ? [] : [request]) as
      | []
      | [unknown];
    const queryFn: QueryFunction<TResult, QueryKey> = () => execute(...input);
    const optionsWithTaggedKey = createQueryOptions<
      TResult,
      LinuxIOError,
      TData,
      QueryKey
    >({
      queryKey: queryKey(...input),
      queryFn,
      ...(options ?? {}),
    });
    return { ...optionsWithTaggedKey, queryFn };
  };

  const endpoint = ((...rawArgs: [] | [unknown]) =>
    execute(...rawArgs)) as CommandEndpoint<[] | [unknown], unknown, TResult>;

  endpoint.queryKey = queryKey;
  endpoint.queryOptions = queryOptions;

  endpoint.useFetcher = (() => {
    const queryClient = useQueryClient();
    return useCallback(
      (...params: unknown[]) =>
        queryClient.fetchQuery(queryOptions<TResult>(...params)),
      [queryClient],
    );
  }) as CommandEndpoint<[] | [unknown], unknown, TResult>["useFetcher"];

  endpoint.useCache = (() => {
    const queryClient = useQueryClient();
    return useMemo(() => {
      const filterKey = (input: [] | [unknown]) =>
        input.length > 0
          ? queryKey(...input)
          : endpointQueryPrefix(routeName(handler, command));
      return {
        get: (...input: [] | [unknown]) =>
          queryClient.getQueryData<TResult>(queryKey(...input)),
        set: (...args: unknown[]) => {
          const input = args.slice(0, -1) as [] | [unknown];
          const updater = args.at(-1) as
            | TResult
            | ((old: TResult | undefined) => TResult | undefined);
          queryClient.setQueryData<TResult>(queryKey(...input), updater);
        },
        invalidate: (...input: [] | [unknown]) =>
          queryClient.invalidateQueries({ queryKey: filterKey(input) }),
        remove: (...input: [] | [unknown]) => {
          queryClient.removeQueries({ queryKey: filterKey(input) });
        },
        cancel: (...input: [] | [unknown]) =>
          queryClient.cancelQueries({ queryKey: filterKey(input) }),
      };
    }, [queryClient]);
  }) as CommandEndpoint<[] | [unknown], unknown, TResult>["useCache"];

  endpoint.useAction = (config?: ActionConfig<unknown, TResult>) => {
    const route = routeName(handler, command);
    assertRouteMode(route, "query");
    return useActionMutation<TResult>(
      route,
      (request: unknown) => execute(request),
      config,
    );
  };

  endpoint.useJobAction = (config?: ActionConfig<unknown, TResult>) => {
    const route = routeName(handler, command);
    assertRouteMode(route, "job");
    return useActionMutation<TResult>(
      route,
      async (request: unknown) => {
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
      config,
    );
  };

  endpoint.useJobStreamAction = (<
    TStreamResult = TResult,
    TProgress = ProgressFrame,
  >(
    config?: JobStreamActionConfig<unknown, TStreamResult, TProgress>,
  ) => {
    const route = routeName(handler, command);
    assertRouteMode(route, "job");
    const attachJobRef = useRef<JobSnapshot | null>(null);
    const mutation = useActionMutation<TStreamResult>(
      route,
      async (request: unknown) => {
        const signal = resolveSignal(config?.signal, request);
        const attachJob = attachJobRef.current;
        attachJobRef.current = null;
        const result = attachJob
          ? attachJob
          : await core.request<TResult>(
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
      config,
    );
    const { mutate } = mutation;
    const attach = useCallback(
      (job: JobSnapshot, variables: unknown) => {
        attachJobRef.current = job;
        mutate(variables);
      },
      [mutate],
    );
    return { ...mutation, attach };
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
