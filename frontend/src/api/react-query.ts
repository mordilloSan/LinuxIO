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
import { useStreamMux } from "./linuxio";
import type {
  LinuxIOSchema,
  HandlerName,
  CommandName,
  CommandResult,
} from "./linuxio-types";

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

  return useQuery<T, LinuxIOError>({
    queryKey: ["linuxio", handler, command, ...args],
    queryFn: () => core.call<T>(handler, command, args),
    enabled: isOpen && (options?.enabled ?? true),
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

function variablesToArgs(variables: unknown): string[] {
  if (variables === undefined || variables === null) {
    return [];
  }
  if (Array.isArray(variables)) {
    return variables.map(String);
  }
  if (typeof variables === "object") {
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
 * Mutation options type
 */
type MutationOptions<TResult> = Omit<
  UseMutationOptions<TResult, LinuxIOError, string[]>,
  "mutationFn"
>;

/**
 * Command endpoint interface
 */
interface CommandEndpoint<TResult> {
  /**
   * React Query hook for fetching data
   * @param args - Command arguments (optional)
   * @param options - React Query options (optional)
   */
  useQuery: (
    ...params: Array<string | QueryOptions<TResult>>
  ) => ReturnType<typeof useQuery<TResult, LinuxIOError>>;

  /**
   * React Query hook for mutations
   * @param options - React Query mutation options (optional)
   */
  useMutation: (
    options?: MutationOptions<TResult>,
  ) => ReturnType<typeof useMutation<TResult, LinuxIOError, string[]>>;
}

/**
 * Create a command endpoint factory
 */
function createEndpoint<TResult>(
  handler: string,
  command: string,
): CommandEndpoint<TResult> {
  return {
    useQuery(...params: Array<string | QueryOptions<TResult>>) {
      const { isOpen } = useStreamMux();

      // Separate args from options
      let args: string[] = [];
      let options: QueryOptions<TResult> | undefined;

      for (const param of params) {
        if (typeof param === "string") {
          args.push(param);
        } else if (param && typeof param === "object") {
          options = param as QueryOptions<TResult>;
        }
      }

      return useQuery<TResult, LinuxIOError>({
        queryKey: ["linuxio", handler, command, ...args],
        queryFn: () => core.call<TResult>(handler, command, args),
        enabled: isOpen && (options?.enabled ?? true),
        ...options,
      });
    },

    useMutation(options?: MutationOptions<TResult>) {
      return useMutation<TResult, LinuxIOError, string[]>({
        mutationFn: (args: string[]) =>
          core.call<TResult>(handler, command, args),
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

/**
 * Create the full typed API via Proxy
 */
function createTypedAPI(): TypedAPI {
  const cache = new Map<string, HandlerEndpoints<HandlerName>>();

  return new Proxy({} as TypedAPI, {
    get(_, handler: string) {
      if (!cache.has(handler)) {
        cache.set(handler, createHandlerNamespace(handler as HandlerName));
      }
      return cache.get(handler);
    },
  });
}

// Create the typed API instance
const typedAPI = createTypedAPI();

// ============================================================================
// Export
// ============================================================================

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
const linuxio = {
  // Type-safe handler namespaces (system, docker, dbus, etc.)
  ...typedAPI,
  // String-based React Query hooks (for modules)
  useCall,
  useMutate,
  // Core API (call, spawn, openStream)
  ...core,
};

export default linuxio;
export { LinuxIOError };

// Re-export types for convenience
export type {
  LinuxIOSchema,
  HandlerName,
  CommandName,
  CommandResult,
} from "./linuxio-types";
