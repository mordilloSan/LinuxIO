/**
 * React Query hooks for stream-based API calls
 *
 * These hooks provide the same interface as useQuery/useMutation
 * but use yamux streams instead of HTTP.
 */

import {
  useQuery,
  useMutation,
  type UseQueryOptions,
  type UseMutationOptions,
} from "@tanstack/react-query";
import { useStreamMux } from "@/hooks/useStreamMux";
import { streamApi, StreamApiError } from "@/utils/streamApi";

/**
 * Options for useStreamQuery
 */
export interface StreamQueryOptions<T> extends Omit<
  UseQueryOptions<T, StreamApiError>,
  "queryKey" | "queryFn"
> {
  /** Handler group (e.g., "system", "docker") */
  handlerType: string;
  /** Handler command (e.g., "get_cpu_info") */
  command: string;
  /** Additional arguments to pass to the handler */
  args?: string[];
}

/**
 * React Query hook for GET-style stream API calls.
 *
 * The query is automatically disabled until the WebSocket multiplexer
 * is connected and ready.
 *
 * @example
 * ```tsx
 * const { data, isPending, error } = useStreamQuery<CPUInfo>({
 *   handlerType: "system",
 *   command: "get_cpu_info",
 *   refetchInterval: 2000,
 * });
 * ```
 */
export function useStreamQuery<T = unknown>({
  handlerType,
  command,
  args,
  enabled = true,
  ...options
}: StreamQueryOptions<T>) {
  const { isOpen } = useStreamMux();

  return useQuery<T, StreamApiError>({
    queryKey: ["stream", handlerType, command, ...(args || [])],
    queryFn: () => streamApi.get<T>(handlerType, command, args),
    // Only enable when mux is open AND caller's enabled condition is true
    enabled: isOpen && enabled,
    ...options,
  });
}

/**
 * Options for useStreamMutation
 */
export interface StreamMutationOptions<TData, TVariables> extends Omit<
  UseMutationOptions<TData, StreamApiError, TVariables>,
  "mutationFn"
> {
  /** Handler group (e.g., "docker") */
  handlerType: string;
  /** Handler command (e.g., "start_container") */
  command: string;
}

/**
 * React Query hook for POST-style stream API mutations.
 *
 * @example
 * ```tsx
 * const { mutate, isPending } = useStreamMutation<void, { id: string }>({
 *   handlerType: "docker",
 *   command: "start_container",
 *   onSuccess: () => toast.success("Container started"),
 * });
 *
 * // Later:
 * mutate({ id: containerId });
 * ```
 */
export function useStreamMutation<TData = unknown, TVariables = unknown>({
  handlerType,
  command,
  ...options
}: StreamMutationOptions<TData, TVariables>) {
  return useMutation<TData, StreamApiError, TVariables>({
    mutationFn: (variables) => {
      // If variables is an array of strings, pass as args
      if (
        Array.isArray(variables) &&
        variables.every((v) => typeof v === "string")
      ) {
        return streamApi.get<TData>(
          handlerType,
          command,
          variables as string[],
        );
      }
      // Otherwise, pass as body
      return streamApi.post<TData>(handlerType, command, variables);
    },
    ...options,
  });
}

/**
 * Hook for mutations where the variable is a single string argument.
 * Useful for operations like start_container where you just pass an ID.
 *
 * @example
 * ```tsx
 * const { mutate } = useStreamMutationWithArg<void>({
 *   handlerType: "docker",
 *   command: "start_container",
 * });
 *
 * // Later:
 * mutate("container-id-123");
 * ```
 */
export function useStreamMutationWithArg<TData = unknown>({
  handlerType,
  command,
  ...options
}: Omit<StreamMutationOptions<TData, string>, never>) {
  return useMutation<TData, StreamApiError, string>({
    mutationFn: (arg) => streamApi.get<TData>(handlerType, command, [arg]),
    ...options,
  });
}

/**
 * Hook for mutations with multiple string arguments.
 *
 * @example
 * ```tsx
 * const { mutate } = useStreamMutationWithArgs<void>({
 *   handlerType: "docker",
 *   command: "some_command",
 * });
 *
 * // Later:
 * mutate(["arg1", "arg2"]);
 * ```
 */
export function useStreamMutationWithArgs<TData = unknown>({
  handlerType,
  command,
  ...options
}: Omit<StreamMutationOptions<TData, string[]>, never>) {
  return useMutation<TData, StreamApiError, string[]>({
    mutationFn: (args) => streamApi.get<TData>(handlerType, command, args),
    ...options,
  });
}
