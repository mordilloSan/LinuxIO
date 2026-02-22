/**
 * LinuxIO API Usage Guidelines:
 *
 * 1. TYPE-SAFE API (PREFERRED for core handlers):
 *    linuxio.docker.start_container.useMutation()
 *    linuxio.filebrowser.resource_get.useQuery()
 *
 * 2. STREAMING API (for progress tracking):
 *    linuxio.spawn("filebrowser", "compress", [...]).progress(...)
 *
 * 3. IMPERATIVE API (contexts/effects/non-hook code):
 *    await linuxio.system.get_capabilities.call()
 *    await queryClient.fetchQuery(linuxio.system.get_capabilities.queryOptions())
 *
 * For truly dynamic handlers/commands, use the core API:
 *    await core.call("handler", "command", [args])
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
import {
  useStreamMux,
  useIsUpdating,
  initStreamMux,
  closeStreamMux,
  waitForStreamMux,
  getStreamMux,
} from "./linuxio";
import type {
  HandlerName,
  CommandName,
  CommandArgs,
  CommandResult,
} from "./linuxio-types";

function serializeArg(arg: unknown): string {
  if (typeof arg === "string") return arg;
  if (typeof arg === "object" || Array.isArray(arg)) return JSON.stringify(arg);
  return String(arg);
}

function serializeArgs(args: readonly unknown[]): string[] {
  return (args ?? []).filter((arg) => arg !== undefined).map(serializeArg);
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

/**
 * Query config with explicit args for complex types
 */
type QueryConfig<TResult> = {
  args?: unknown[];
} & QueryOptions<TResult>;

type SelectableQueryConfig<TResult, TData = TResult> = {
  args?: unknown[];
} & SelectableQueryOptions<TResult, TData>;

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

function parseQueryParams<TResult>(
  params: (string | QueryOptions<TResult> | QueryConfig<TResult>)[],
): { args: unknown[]; options: QueryOptions<TResult> | undefined } {
  let args: unknown[] = [];
  let options: QueryOptions<TResult> | undefined;

  if (
    params.length === 1 &&
    params[0] &&
    typeof params[0] === "object" &&
    "args" in params[0]
  ) {
    const { args: explicitArgs, ...rest } = params[0] as QueryConfig<TResult>;
    args = explicitArgs ?? [];
    options = rest;
    return { args, options };
  }

  for (const param of params) {
    if (typeof param === "string") {
      args.push(param);
    } else if (param && typeof param === "object") {
      options = param as QueryOptions<TResult>;
    }
  }

  return { args, options };
}

function parseSelectableQueryParams<TResult, TData = TResult>(
  params: (
    | string
    | SelectableQueryOptions<TResult, TData>
    | SelectableQueryConfig<TResult, TData>
  )[],
): {
  args: unknown[];
  options: SelectableQueryOptions<TResult, TData> | undefined;
} {
  let args: unknown[] = [];
  let options: SelectableQueryOptions<TResult, TData> | undefined;

  if (
    params.length === 1 &&
    params[0] &&
    typeof params[0] === "object" &&
    "args" in params[0]
  ) {
    const { args: explicitArgs, ...rest } = params[0] as SelectableQueryConfig<
      TResult,
      TData
    >;
    args = explicitArgs ?? [];
    options = rest;
    return { args, options };
  }

  for (const param of params) {
    if (typeof param === "string") {
      args.push(param);
    } else if (param && typeof param === "object") {
      options = param as SelectableQueryOptions<TResult, TData>;
    }
  }

  return { args, options };
}

/**
 * Create a command endpoint factory
 */
function createEndpoint<TResult>(
  handler: string,
  command: string,
): CommandEndpoint<TResult> {
  const queryKey = (...rawArgs: unknown[]): QueryKey => {
    const serialized = serializeArgs(rawArgs);
    return ["linuxio", handler, command, ...serialized] as const;
  };

  const call = (...rawArgs: unknown[]): Promise<TResult> => {
    const serialized = serializeArgs(rawArgs);
    return core.call<TResult>(handler, command, serialized);
  };

  const queryOptions = (
    ...params: (string | QueryOptions<TResult> | QueryConfig<TResult>)[]
  ): UseQueryOptions<TResult, LinuxIOError> => {
    const { args, options } = parseQueryParams<TResult>(params);
    const serializedArgs = serializeArgs(args);

    return {
      queryKey: ["linuxio", handler, command, ...serializedArgs],
      queryFn: () => core.call<TResult>(handler, command, serializedArgs),
      ...(options ?? {}),
    };
  };

  const queryOptionsWithSelect = <TData = TResult>(
    ...params: (
      | string
      | SelectableQueryOptions<TResult, TData>
      | SelectableQueryConfig<TResult, TData>
    )[]
  ): UseQueryOptions<TResult, LinuxIOError, TData> => {
    const { args, options } = parseSelectableQueryParams<TResult, TData>(
      params,
    );
    const serializedArgs = serializeArgs(args);

    return {
      queryKey: ["linuxio", handler, command, ...serializedArgs],
      queryFn: () => core.call<TResult>(handler, command, serializedArgs),
      ...(options ?? {}),
    };
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
          return core.call<TResult>(handler, command, serializedArgs);
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

// Static methods that exist on linuxio directly
const staticMethods = {
  spawn: core.spawn,
  openStream: core.openStream,
  LinuxIOError: core.LinuxIOError,
  SpawnedProcess: core.SpawnedProcess,
};

// Handler namespace cache
const handlerCache = new Map<string, HandlerEndpoints<HandlerName>>();

/**
 * LinuxIO API
 *
 * @example
 * // TYPE-SAFE API (for built-in handlers)
 * const { data } = linuxio.system.get_drive_info.useQuery();
 * const { mutate } = linuxio.docker.start_container.useMutation();
 *
 * // CORE API (non-React, Promise-based)
 * const drives = await linuxio.system.get_drive_info.call();
 * const result = await linuxio.spawn("filebrowser", "compress", [...])
 *   .progress(p => setProgress(p.pct));
 */
const linuxio = new Proxy(staticMethods as typeof staticMethods & TypedAPI, {
  get(target, prop: string) {
    // First check static methods
    if (prop in target) {
      return (target as Record<string, unknown>)[prop];
    }
    // `linuxio.call()` alias is intentionally removed.
    if (prop === "call") {
      return undefined;
    }
    // Then return handler namespace (lazily created)
    if (!handlerCache.has(prop)) {
      handlerCache.set(prop, createHandlerNamespace(prop as HandlerName));
    }
    return handlerCache.get(prop);
  },
});

export default linuxio;
export { LinuxIOError };

// Re-export mux lifecycle functions for convenience
export { initStreamMux, closeStreamMux, waitForStreamMux, getStreamMux };

// Re-export types for convenience
export type {
  LinuxIOSchema,
  HandlerName,
  CommandName,
  CommandResult,
} from "./linuxio-types";
