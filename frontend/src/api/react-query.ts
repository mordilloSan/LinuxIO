/**
 * LinuxIO React Query API
 *
 * Two ways to call the API:
 *
 * 1. TYPE-SAFE API (recommended for built-in handlers)
 *    Full autocomplete and compile-time type checking.
 *
 *    const { data } = linuxio.system.get_drive_info.useQuery();
 *    const { data } = linuxio.docker.list_containers.useQuery();
 *    const { mutate } = linuxio.docker.start_container.useMutation();
 *
 * 2. STRING-BASED API (for modules and dynamic handlers)
 *    Use when handler/command names are dynamic or for module calls.
 *
 *    const { data } = linuxio.useCall("module.weather", "getForecast", ["London"]);
 *    const { mutate } = linuxio.useMutate("module.lights", "toggle");
 */

import {
  useQuery,
  useMutation,
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
import type { HandlerName, CommandName, CommandResult } from "./linuxio-types";

// ============================================================================
// String-based API (for dynamic handlers like modules)
// ============================================================================

/**
 * React Query hook for dynamic/module handlers
 *
 * Use this for:
 * - Module handlers: linuxio.useCall("module.myModule", "command", [])
 * - Dynamic handlers not in the type schema
 *
 * For built-in handlers, prefer the type-safe API:
 * - linuxio.system.get_drive_info.useQuery()
 *
 * @example
 * const { data } = linuxio.useCall("module.weather", "getForecast", ["London"]);
 */
export function useCall<T = unknown>(
  handler: string,
  command: string,
  args: string[] = [],
  options?: Omit<UseQueryOptions<T, LinuxIOError>, "queryKey" | "queryFn">,
) {
  const { isOpen } = useStreamMux();
  const isUpdating = useIsUpdating();

  return useQuery<T, LinuxIOError>({
    queryKey: ["linuxio", handler, command, ...args],
    queryFn: () => core.call<T>(handler, command, args),
    enabled: isOpen && !isUpdating && (options?.enabled ?? true),
    ...options,
  });
}

/**
 * React Query mutation hook for dynamic/module handlers
 *
 * Use this for:
 * - Module mutations: linuxio.useMutate("module.myModule", "action")
 * - Dynamic handlers not in the type schema
 *
 * For built-in handlers, prefer the type-safe API:
 * - linuxio.docker.start_container.useMutation()
 *
 * @example
 * const { mutate } = linuxio.useMutate("module.lights", "toggle");
 * mutate({ roomId: "living-room" });
 */
export function useMutate<TData = unknown, TVariables = void>(
  handler: string,
  command: string,
  options?: Omit<
    UseMutationOptions<TData, LinuxIOError, TVariables>,
    "mutationFn"
  >,
) {
  return useMutation<TData, LinuxIOError, TVariables>({
    mutationFn: (variables: TVariables) => {
      const args = variablesToArgs(variables);
      return core.call<TData>(handler, command, args);
    },
    ...options,
  });
}

/**
 * Convert mutation variables to string array arguments.
 *
 * WARNING: When passing objects, argument order depends on property insertion order.
 * For positional arguments, prefer passing an array: mutate(["arg1", "arg2", "arg3"])
 *
 * @param variables - Mutation variables (array, object, primitive, or void)
 * @returns Array of string arguments
 */
function variablesToArgs(variables: unknown): string[] {
  if (variables === undefined || variables === null) {
    return [];
  }
  if (Array.isArray(variables)) {
    return variables.map(String);
  }
  if (typeof variables === "object") {
    // WARNING: Object.values() order is insertion order (not guaranteed in all JS engines)
    // For positional arguments, use arrays instead: mutate(["arg1", "arg2"])
    return Object.values(variables as Record<string, unknown>).map(String);
  }
  return [String(variables)];
}

// ============================================================================
// Type-Safe API
// ============================================================================

/**
 * Query options type
 */
type QueryOptions<TResult> = Omit<
  UseQueryOptions<TResult, LinuxIOError>,
  "queryKey" | "queryFn"
>;

/**
 * Query config with explicit args for complex types
 */
type QueryConfig<TResult> = {
  args?: unknown[];
} & QueryOptions<TResult>;

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
    ...params: Array<string | QueryOptions<TResult> | QueryConfig<TResult>>
  ) => ReturnType<typeof useQuery<TResult, LinuxIOError>>;

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

/**
 * Create a command endpoint factory
 */
function createEndpoint<TResult>(
  handler: string,
  command: string,
): CommandEndpoint<TResult> {
  return {
    useQuery(
      ...params: Array<string | QueryOptions<TResult> | QueryConfig<TResult>>
    ) {
      const { isOpen } = useStreamMux();
      const isUpdating = useIsUpdating();

      let args: unknown[] = [];
      let options: QueryOptions<TResult> | undefined;

      // Check if first param is a config object with explicit args
      if (
        params.length === 1 &&
        params[0] &&
        typeof params[0] === "object" &&
        "args" in params[0]
      ) {
        const config = params[0] as QueryConfig<TResult>;
        args = config.args ?? [];
        options = config;
      } else {
        // Legacy mode: separate string args from options
        for (const param of params) {
          if (typeof param === "string") {
            args.push(param);
          } else if (param && typeof param === "object") {
            options = param as QueryOptions<TResult>;
          }
        }
      }

      // Serialize args to strings (JSON for complex types)
      const serializedArgs = args.map((arg) => {
        if (typeof arg === "string") {
          return arg;
        }
        if (typeof arg === "object" || Array.isArray(arg)) {
          return JSON.stringify(arg);
        }
        return String(arg);
      });

      return useQuery<TResult, LinuxIOError>({
        queryKey: ["linuxio", handler, command, ...serializedArgs],
        queryFn: () =>
          core.call<TResult>(handler, command, serializedArgs as string[]),
        enabled: isOpen && !isUpdating && (options?.enabled ?? true),
        ...options,
      });
    },

    useMutation(options?: MutationOptions<TResult>) {
      return useMutation<TResult, LinuxIOError, unknown[]>({
        mutationFn: (args: unknown[]) => {
          // Serialize args to strings (JSON for complex types)
          const serializedArgs = (args ?? []).map((arg) => {
            if (typeof arg === "string") {
              return arg;
            }
            if (typeof arg === "object" || Array.isArray(arg)) {
              return JSON.stringify(arg);
            }
            return String(arg);
          });
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
  [C in CommandName<H>]: CommandEndpoint<CommandResult<H, C>>;
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
  useCall,
  useMutate,
  ...core,
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
 * // STRING-BASED API (for modules)
 * const { data } = linuxio.useCall("module.weather", "getForecast", ["London"]);
 * const { mutate } = linuxio.useMutate("module.lights", "toggle");
 *
 * // CORE API (non-React, Promise-based)
 * const drives = await linuxio.call("system", "get_drive_info");
 * const result = await linuxio.spawn("filebrowser", "compress", [...])
 *   .progress(p => setProgress(p.pct));
 */
const linuxio = new Proxy(staticMethods as typeof staticMethods & TypedAPI, {
  get(target, prop: string) {
    // First check static methods
    if (prop in target) {
      return (target as Record<string, unknown>)[prop];
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
