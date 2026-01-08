/**
 * Optional React Query wrappers for LinuxIO API
 *
 * These hooks provide React Query integration for the core LinuxIO API.
 * Use these if you want automatic caching, refetching, and state management.
 * Or build your own wrappers - the core API is framework-agnostic.
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

/**
 * React Query hook for simple calls
 *
 * @example
 * const { data, isLoading, error } = linuxio.useCall<ApiDisk[]>("system", "get_drive_info");
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
 * React Query hook for mutations
 *
 * @example
 * const { mutate } = linuxio.useMutate("docker", "start_container");
 * mutate({ containerId: "abc123" });
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
      // Convert variables to args array
      const args = variablesToArgs(variables);
      return core.call<TData>(handler, command, args);
    },
    ...options,
  });
}

/**
 * Helper to convert mutation variables to string array args
 */
function variablesToArgs(variables: any): string[] {
  if (variables === undefined || variables === null) {
    return [];
  }
  if (Array.isArray(variables)) {
    return variables.map(String);
  }
  if (typeof variables === "object") {
    // If object has specific keys, use them in order
    // Otherwise convert all values
    return Object.values(variables).map(String);
  }
  return [String(variables)];
}

/**
 * LinuxIO API - React Query integration
 *
 * @example
 * import linuxio from "@/api/react-query";
 *
 * const { data } = linuxio.useCall("system", "get_drive_info");
 * const { mutate } = linuxio.useMutate("docker", "start_container");
 */
const linuxio = {
  useCall,
  useMutate,
  // Re-export core API
  ...core,
};

export default linuxio;
export { LinuxIOError };
