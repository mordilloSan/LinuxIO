import {
  type QueryFunction,
  type QueryKey,
  useMutation,
  type UseMutationOptions,
  type UseMutationResult,
  type UseQueryOptions,
  useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";

import type { ToastMeta } from "@/types/navigation";
import { getMutationErrorMessage } from "@/utils/mutations";

import { call, splitCallRoute } from "./calls";
import type {
  CallRequest,
  CallResult,
  CallRoute,
  NoRequestCallRoute,
  RequestCallRoute,
} from "./generated/linuxio-types";
import { LinuxIOError } from "./linuxio-core";
import { OPERATION_QUERY_INVALIDATIONS } from "./operation-query-invalidations";

// Shared freshness presets for TanStack Query descriptors.
export const CACHE_TTL_MS = {
  NONE: 0,
  TWO_SECONDS: 2_000,
  FIVE_SECONDS: 5_000,
  THIRTY_SECONDS: 30_000,
  ONE_MINUTE: 60_000,
  FIVE_MINUTES: 5 * 60 * 1000,
  ONE_DAY: 24 * 60 * 60 * 1000,
} as const;

export type CallQueryOptions<R extends CallRoute, TData = CallResult<R>> = Omit<
  UseQueryOptions<CallResult<R>, LinuxIOError, TData>,
  "queryKey" | "queryFn"
>;

/** A concrete TanStack Query descriptor for one Call invocation. */
export type CallDescriptor<
  R extends CallRoute,
  TData = CallResult<R>,
> = UseQueryOptions<CallResult<R>, LinuxIOError, TData> & {
  queryFn: QueryFunction<CallResult<R>, QueryKey>;
  queryKey: QueryKey;
  readonly route: R;
};

/** Descriptor factory for a Call whose request is supplied at the callsite. */
export type CallFactory<R extends RequestCallRoute> = {
  (request: CallRequest<R>): CallDescriptor<R>;
  readonly route: R;
};

/** Generated namespace value for a migrated Call route. */
export type CallDefinition<R extends CallRoute> = R extends NoRequestCallRoute
  ? CallDescriptor<R>
  : R extends RequestCallRoute
    ? CallFactory<R>
    : never;

/** Build a descriptor that can be passed directly to useQuery/useSuspenseQuery. */
export function defineCall<R extends NoRequestCallRoute>(
  route: R,
): CallDescriptor<R> {
  const [handler, command] = splitCallRoute(route);
  const queryFn: QueryFunction<CallResult<R>, QueryKey> = ({ signal }) =>
    call(route, undefined, { signal });
  return {
    queryFn,
    queryKey: ["linuxio", handler, command],
    route,
  };
}

/** Build a request-bound descriptor factory for a migrated Call route. */
export function defineCallWithRequest<R extends RequestCallRoute>(
  route: R,
): CallFactory<R> {
  const [handler, command] = splitCallRoute(route);
  return Object.assign(
    (request: CallRequest<R>): CallDescriptor<R> => {
      const queryFn: QueryFunction<CallResult<R>, QueryKey> = ({ signal }) =>
        call(route, request, { signal });
      return {
        queryFn,
        queryKey: ["linuxio", handler, command, request],
        route,
      };
    },
    { route },
  );
}

type MutationOptions<TRequest, TResult> = Omit<
  UseMutationOptions<TResult, LinuxIOError, TRequest>,
  "mutationFn"
>;

/** Declarative cache invalidation and feedback for bounded and Task mutations. */
export interface ActionConfig<TRequest, TResult> {
  /**
   * Query keys to invalidate after success. Defaults to the route's operation
   * manifest entry; pass `[]` to opt out or a value to override it.
   */
  invalidates?:
    | QueryKey[]
    | ((result: TResult, variables: TRequest) => QueryKey[]);
  /** Success toast text, or a callback for custom success handling. */
  success?: string | ((result: TResult, variables: TRequest) => void);
  /** A successful result warning that replaces a string-form success toast. */
  warning?: (result: TResult, variables: TRequest) => string | null | undefined;
  /** Error toast fallback text, or a callback for custom error handling. */
  error?: string | ((error: LinuxIOError, variables: TRequest) => void);
  /** Typed navigation target attached to string-form feedback. */
  toast?: ToastMeta;
  /** Raw TanStack mutation options; callbacks run after the config callbacks. */
  options?: MutationOptions<TRequest, TResult>;
}

interface CallTarget<R extends CallRoute> {
  readonly route: R;
}

/** Run a migrated Call as a TanStack mutation. */
export function useCallMutation<R extends CallRoute>(
  endpoint: CallTarget<R>,
  config?: ActionConfig<CallRequest<R>, CallResult<R>>,
): UseMutationResult<CallResult<R>, LinuxIOError, CallRequest<R>> {
  // The overloads on `call` enforce this pairing at public callsites. Here the
  // generated endpoint's route literal already binds the exact request/result.
  const invoke = call as (
    route: R,
    request: CallRequest<R>,
  ) => Promise<CallResult<R>>;
  return useActionMutation<CallResult<R>, CallRequest<R>>(
    endpoint.route,
    (request) => invoke(endpoint.route, request),
    config,
  );
}

/** Shared TanStack mutation lifecycle for Calls and Tasks. */
export function useActionMutation<TResult, TRequest = unknown>(
  route: string,
  mutationFn: (request: TRequest) => Promise<TResult>,
  config: ActionConfig<TRequest, TResult> | undefined,
): UseMutationResult<TResult, LinuxIOError, TRequest> {
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

  return useMutation<TResult, LinuxIOError, TRequest>({
    mutationFn,
    ...options,
    onSuccess: async (result, variables, onMutateResult, context) => {
      const keys =
        typeof invalidates === "function"
          ? invalidates(result, variables)
          : (invalidates ?? OPERATION_QUERY_INVALIDATIONS[route] ?? []);
      await Promise.all(
        keys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      );
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
