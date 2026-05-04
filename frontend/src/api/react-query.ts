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
 *    await linuxio.system.get_capabilities.call()
 *    await queryClient.fetchQuery(linuxio.system.get_capabilities.queryOptions())
 *
 */

import {
  useQuery,
  useMutation,
  type QueryKey,
  type UseQueryOptions,
  type UseMutationOptions,
} from "@tanstack/react-query";

import * as core from "./linuxio-core";
import { LinuxIOError } from "./linuxio-core";
import { useStreamMux, useIsUpdating } from "./linuxio";
import type {
  HandlerName,
  CommandName,
  CommandArgs,
  CommandResult,
} from "./linuxio-types";

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
): core.CallOptions["retryPolicy"] {
  if (
    RETRYABLE_COMMAND_PREFIXES.some((prefix) => command.startsWith(prefix)) ||
    RETRYABLE_COMMANDS.has(`${handler}.${command}`)
  ) {
    return "connection_closed";
  }
  return "none";
}

function serializeArg(arg: unknown): string {
  if (arg === undefined) return "";
  if (typeof arg === "string") return arg;
  if (typeof arg === "object") return JSON.stringify(arg);
  return String(arg);
}

function serializeArgs(args: readonly unknown[]): string[] {
  return (args ?? []).map(serializeArg);
}

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

type ArgsConfig<TOptions> = {
  args?: unknown[];
} & TOptions;

/**
 * Query config with explicit args for complex types
 */
type QueryConfig<TResult> = ArgsConfig<QueryOptions<TResult>>;

type SelectableQueryConfig<TResult, TData = TResult> = ArgsConfig<
  SelectableQueryOptions<TResult, TData>
>;

/**
 * Mutation options type - accepts unknown[] to support complex types
 */
type MutationOptions<TResult> = Omit<
  UseMutationOptions<TResult, LinuxIOError, unknown[]>,
  "mutationFn"
>;

/**
 * Command endpoint interface
 */
interface CommandEndpoint<TResult> {
  /**
   * Framework-agnostic call (Promise-based) using the same argument serialization
   * and cache key scheme as the React Query hooks.
   */
  call: (...args: unknown[]) => Promise<TResult>;

  /** Deterministic React Query key for this command */
  queryKey: (...args: unknown[]) => QueryKey;

  /**
   * React Query options for `queryClient.fetchQuery/ensureQueryData`
   * and non-hook integration points.
   */
  queryOptions: (
    ...params: (string | QueryOptions<TResult> | QueryConfig<TResult>)[]
  ) => UseQueryOptions<TResult, LinuxIOError>;

  /**
   * React Query options with support for transformed `select` output data.
   */
  queryOptionsWithSelect: <TData = TResult>(
    ...params: (
      | string
      | SelectableQueryOptions<TResult, TData>
      | SelectableQueryConfig<TResult, TData>
    )[]
  ) => UseQueryOptions<TResult, LinuxIOError, TData>;

  /**
   * React Query hook for fetching data
   *
   * @example
   * // No arguments
   * useQuery()
   *
   * @example
   * // String arguments
   * useQuery("arg1", "arg2")
   *
   * @example
   * // String arguments with options
   * useQuery("arg1", { staleTime: 60000 })
   *
   * @example
   * // Complex arguments (objects, arrays) with explicit args
   * useQuery({ args: ["arg1", { complex: "object" }], staleTime: 60000 })
   */
  useQuery: (
    ...params: (string | QueryOptions<TResult> | QueryConfig<TResult>)[]
  ) => ReturnType<typeof useQuery<TResult, LinuxIOError>>;

  /**
   * React Query hook with support for transformed `select` output data.
   */
  useQueryWithSelect: <TData = TResult>(
    ...params: (
      | string
      | SelectableQueryOptions<TResult, TData>
      | SelectableQueryConfig<TResult, TData>
    )[]
  ) => ReturnType<typeof useQuery<TResult, LinuxIOError, TData>>;

  /**
   * React Query hook for mutations
   *
   * @example
   * // Mutate with string args
   * const { mutate } = useMutation();
   * mutate(["arg1", "arg2"]);
   *
   * @example
   * // Mutate with complex args (objects, arrays)
   * const { mutate } = useMutation();
   * mutate(["arg1", { complex: "object" }]);
   */
  useMutation: (
    options?: MutationOptions<TResult>,
  ) => ReturnType<typeof useMutation<TResult, LinuxIOError, unknown[]>>;
}

function hasExplicitArgs(value: unknown): value is { args?: unknown[] } {
  return !!value && typeof value === "object" && "args" in value;
}

function parseQueryParams<TOptions extends object>(
  params: (string | TOptions | ArgsConfig<TOptions>)[],
): { args: unknown[]; options: TOptions | undefined } {
  let args: unknown[] = [];
  let options: TOptions | undefined;

  if (params.length === 1 && hasExplicitArgs(params[0])) {
    const { args: explicitArgs, ...rest } = params[0] as ArgsConfig<TOptions>;
    args = explicitArgs ?? [];
    options = rest as TOptions;
    return { args, options };
  }

  for (const param of params) {
    if (typeof param === "string") {
      args.push(param);
    } else if (param && typeof param === "object") {
      options = param as TOptions;
    }
  }

  return { args, options };
}

function buildQueryOptions<TResult, TData = TResult>(
  handler: string,
  command: string,
  rawArgs: unknown[],
  options?: SelectableQueryOptions<TResult, TData>,
): UseQueryOptions<TResult, LinuxIOError, TData> {
  const serializedArgs = serializeArgs(rawArgs);

  return {
    queryKey: ["linuxio", handler, command, ...serializedArgs],
    queryFn: () =>
      core.call<TResult>(handler, command, serializedArgs, {
        retryPolicy: getRetryPolicy(handler, command),
      }),
    ...(options ?? {}),
  };
}

/**
 * Create a command endpoint factory
 */
function createEndpoint<TResult>(
  handler: string,
  command: string,
): CommandEndpoint<TResult> {
  const retryPolicy = getRetryPolicy(handler, command);
  const queryKey = (...rawArgs: unknown[]): QueryKey => {
    const serialized = serializeArgs(rawArgs);
    return ["linuxio", handler, command, ...serialized] as const;
  };

  const call = (...rawArgs: unknown[]): Promise<TResult> => {
    const serialized = serializeArgs(rawArgs);
    return core.call<TResult>(handler, command, serialized, { retryPolicy });
  };

  const queryOptions = (
    ...params: (string | QueryOptions<TResult> | QueryConfig<TResult>)[]
  ): UseQueryOptions<TResult, LinuxIOError> => {
    const { args, options } = parseQueryParams<QueryOptions<TResult>>(params);
    return buildQueryOptions<TResult>(handler, command, args, options);
  };

  const queryOptionsWithSelect = <TData = TResult>(
    ...params: (
      | string
      | SelectableQueryOptions<TResult, TData>
      | SelectableQueryConfig<TResult, TData>
    )[]
  ): UseQueryOptions<TResult, LinuxIOError, TData> => {
    const { args, options } =
      parseQueryParams<SelectableQueryOptions<TResult, TData>>(params);
    return buildQueryOptions<TResult, TData>(handler, command, args, options);
  };

  return {
    call,
    queryKey,
    queryOptions,
    queryOptionsWithSelect,
    useQuery(
      ...params: (string | QueryOptions<TResult> | QueryConfig<TResult>)[]
    ) {
      const { isOpen } = useStreamMux();
      const isUpdating = useIsUpdating();

      const baseOptions = queryOptions(...params);
      return useQuery<TResult, LinuxIOError>({
        ...baseOptions,
        enabled:
          isOpen && !isUpdating && (baseOptions.enabled ?? true) === true,
      });
    },

    useQueryWithSelect<TData = TResult>(
      ...params: (
        | string
        | SelectableQueryOptions<TResult, TData>
        | SelectableQueryConfig<TResult, TData>
      )[]
    ) {
      const { isOpen } = useStreamMux();
      const isUpdating = useIsUpdating();

      const baseOptions = queryOptionsWithSelect<TData>(...params);
      return useQuery<TResult, LinuxIOError, TData>({
        ...baseOptions,
        enabled:
          isOpen && !isUpdating && (baseOptions.enabled ?? true) === true,
      });
    },

    useMutation(options?: MutationOptions<TResult>) {
      return useMutation<TResult, LinuxIOError, unknown[]>({
        mutationFn: (args: unknown[]) => {
          const serializedArgs = serializeArgs(args ?? []);
          return core.call<TResult>(handler, command, serializedArgs, {
            retryPolicy,
          });
        },
        ...options,
      });
    },
  };
}

// ============================================================================
// Handler Namespace Types
// ============================================================================

/**
 * Maps a handler's commands to their endpoints
 */
type HandlerEndpoints<H extends HandlerName> = {
  [C in CommandName<H>]: CommandEndpoint<CommandResult<H, C>> & {
    call: (...args: CommandArgs<H, C>) => Promise<CommandResult<H, C>>;
    queryKey: (...args: CommandArgs<H, C>) => QueryKey;
    queryOptions: (
      ...params: (
        | string
        | QueryOptions<CommandResult<H, C>>
        | QueryConfig<CommandResult<H, C>>
      )[]
    ) => UseQueryOptions<CommandResult<H, C>, LinuxIOError>;
    queryOptionsWithSelect: <TData = CommandResult<H, C>>(
      ...params: (
        | string
        | SelectableQueryOptions<CommandResult<H, C>, TData>
        | SelectableQueryConfig<CommandResult<H, C>, TData>
      )[]
    ) => UseQueryOptions<CommandResult<H, C>, LinuxIOError, TData>;
    useQueryWithSelect: <TData = CommandResult<H, C>>(
      ...params: (
        | string
        | SelectableQueryOptions<CommandResult<H, C>, TData>
        | SelectableQueryConfig<CommandResult<H, C>, TData>
      )[]
    ) => ReturnType<typeof useQuery<CommandResult<H, C>, LinuxIOError, TData>>;
  };
};

/**
 * Full typed API structure
 */
type TypedAPI = {
  [H in HandlerName]: HandlerEndpoints<H>;
};

/**
 * Create handler namespace via Proxy
 */
function createHandlerNamespace<H extends HandlerName>(
  handler: H,
): HandlerEndpoints<H> {
  const cache = new Map<string, CommandEndpoint<unknown>>();

  return new Proxy({} as HandlerEndpoints<H>, {
    get(_, command: string) {
      if (!cache.has(command)) {
        cache.set(command, createEndpoint(handler, command));
      }
      return cache.get(command);
    },
  });
}

// ============================================================================
// Export
// ============================================================================

// Handler namespace cache
const handlerCache = new Map<string, HandlerEndpoints<HandlerName>>();

/**
 * LinuxIO API
 *
 * @example
 * // TYPE-SAFE API (for built-in handlers)
 * const { data } = linuxio.storage.get_drive_info.useQuery();
 * const { mutate } = linuxio.docker.start_container.useMutation();
 *
 * // CORE API (non-React, Promise-based)
 * const drives = await linuxio.storage.get_drive_info.call();
 */
const linuxio = new Proxy({} as TypedAPI, {
  get(_, prop: string) {
    // `linuxio.call()` alias is intentionally removed.
    if (prop === "call") {
      return undefined;
    }
    // Return handler namespace (lazily created)
    if (!handlerCache.has(prop)) {
      handlerCache.set(prop, createHandlerNamespace(prop as HandlerName));
    }
    return handlerCache.get(prop);
  },
});

export default linuxio;

// Re-export types for convenience
export type {
  LinuxIOSchema,
  HandlerName,
  CommandName,
  CommandResult,
} from "./linuxio-types";
