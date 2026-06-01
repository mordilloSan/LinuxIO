/**
 * LinuxIO API Usage Guidelines:
 *
 * 1. TYPE-SAFE API (preferred for built-in handlers):
 *    linuxio.docker.start_container.useMutation()
 *    linuxio.filebrowser.resource_get.useQuery()
 *
 * 2. STREAMING API (for progress tracking):
 *    openTerminalStream(), openJobDataStream(), etc. from @/api
 *
 * 3. IMPERATIVE API (contexts/effects/non-hook code):
 *    await linuxio.system.get_capabilities()
 *    await queryClient.fetchQuery(linuxio.system.get_capabilities.queryOptions())
 *
 */

import {
  type QueryKey,
  useMutation,
  type UseMutationResult,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from "@tanstack/react-query";

import type {
  CommandInput,
  CommandName,
  CommandRequest,
  CommandResult,
  HandlerName,
} from "./generated/linuxio-types";

import { getRouteMode, routeName } from "./generated/route-metadata";
import { isJobSnapshot, waitForJobCompletion } from "./jobs";
import { useIsUpdating, useStreamMux } from "./linuxio";
import * as core from "./linuxio-core";
import { LinuxIOError } from "./linuxio-core";

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
  | { kind: "none" }
  | { kind: "object" }
  | { kind: "field"; field: string };

// ============================================================================
// Type-Safe API
// ============================================================================

/**
 * Query options type
 */
type QueryOptions<TResult> = Omit<
  UseQueryOptions<TResult, LinuxIOError, TResult>,
  "queryKey" | "queryFn"
>;

type SelectableQueryOptions<TResult, TData = TResult> = Omit<
  UseQueryOptions<TResult, LinuxIOError, TData>,
  "queryKey" | "queryFn"
>;

type MutationOptions<TRequest, TResult> = Omit<
  UseMutationOptions<TResult, LinuxIOError, TRequest>,
  "mutationFn"
>;

type QueryOptionsArgs<
  TInput extends readonly unknown[],
  TResult,
> = TInput extends readonly []
  ? [options?: QueryOptions<TResult>]
  : [...input: TInput, options?: QueryOptions<TResult>];

type SelectableQueryOptionsArgs<
  TInput extends readonly unknown[],
  TResult,
  TData,
> = TInput extends readonly []
  ? [options?: SelectableQueryOptions<TResult, TData>]
  : [...input: TInput, options?: SelectableQueryOptions<TResult, TData>];

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
  queryOptions: (
    ...params: QueryOptionsArgs<TInput, TResult>
  ) => UseQueryOptions<TResult, LinuxIOError>;

  /**
   * React Query options with support for transformed `select` output data.
   */
  queryOptionsWithSelect: <TData = TResult>(
    ...params: SelectableQueryOptionsArgs<TInput, TResult, TData>
  ) => UseQueryOptions<TResult, LinuxIOError, TData>;

  /**
   * React Query hook for mutations
   *
   * @example
   * const { mutate } = useMutation();
   * mutate({ containerId });
   */
  useMutation: (
    options?: MutationOptions<TRequest, TResult>,
  ) => UseMutationResult<TResult, LinuxIOError, TRequest>;

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
  useQuery: (
    ...params: QueryOptionsArgs<TInput, TResult>
  ) => ReturnType<typeof useQuery<TResult, LinuxIOError>>;

  /**
   * React Query hook with support for transformed `select` output data.
   */
  useQueryWithSelect: <TData = TResult>(
    ...params: SelectableQueryOptionsArgs<TInput, TResult, TData>
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
  options?: SelectableQueryOptions<TResult, TData>,
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

  const queryOptions = (
    ...params: unknown[]
  ): UseQueryOptions<TResult, LinuxIOError> => {
    const { request, options } = queryRequestAndOptions<QueryOptions<TResult>>(
      requestShape,
      params,
    );
    return buildQueryOptions<TResult>(
      handler,
      command,
      requestShape,
      request,
      options,
    );
  };

  const queryOptionsWithSelect = <TData = TResult>(
    ...params: unknown[]
  ): UseQueryOptions<TResult, LinuxIOError, TData> => {
    const { request, options } = queryRequestAndOptions<
      SelectableQueryOptions<TResult, TData>
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
  endpoint.queryOptionsWithSelect = queryOptionsWithSelect;
  endpoint.useQuery = (
    ...params: unknown[]
  ): ReturnType<typeof useQuery<TResult, LinuxIOError>> => {
    const { isOpen } = useStreamMux();
    const isUpdating = useIsUpdating();

    const baseOptions = queryOptions(...params);
    return useQuery<TResult, LinuxIOError>({
      ...baseOptions,
      enabled: isOpen && !isUpdating && (baseOptions.enabled ?? true) === true,
    });
  };

  endpoint.useQueryWithSelect = (<TData = TResult>(
    ...params: unknown[]
  ): ReturnType<typeof useQuery<TResult, LinuxIOError, TData>> => {
    const { isOpen } = useStreamMux();
    const isUpdating = useIsUpdating();

    const baseOptions = queryOptionsWithSelect<TData>(...params);
    return useQuery<TResult, LinuxIOError, TData>({
      ...baseOptions,
      enabled: isOpen && !isUpdating && (baseOptions.enabled ?? true) === true,
    });
  }) as CommandEndpoint<[] | [unknown], unknown, TResult>["useQueryWithSelect"];

  endpoint.useMutation = (options?: MutationOptions<unknown, TResult>) => {
    const route = routeName(handler, command);
    const mode = getRouteMode(route);
    if (mode && mode !== "job") {
      throw new LinuxIOError(
        `Route ${route} is ${mode}, not mutation/job`,
        "invalid_route_mode",
      );
    }

    return useMutation<TResult, LinuxIOError, unknown>({
      mutationFn: async (request: unknown) => {
        const result = await core.request<TResult>(
          handler,
          command,
          requestForWire(requestShape, request),
          { retryPolicy },
        );
        if (isJobSnapshot(result)) {
          return (await waitForJobCompletion(result)) as TResult;
        }
        return result;
      },
      ...options,
    });
  };

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
