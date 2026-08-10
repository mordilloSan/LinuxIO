import type {
  CallRequest,
  CallResult,
  CallRoute,
  NoRequestCallRoute,
  RequestCallRoute,
} from "./generated/linuxio-types";
import { isRetrySafeCall } from "./generated/route-metadata";
import * as core from "./linuxio-core";

export function getRetryPolicy(
  route: string,
): core.RequestOptions["retryPolicy"] {
  return isRetrySafeCall(route) ? "connection_loss" : "none";
}

export function splitCallRoute(route: string): [string, string] {
  const separator = route.indexOf(".");
  if (separator <= 0 || separator === route.length - 1) {
    throw new Error(`Invalid API route: ${route}`);
  }
  return [route.slice(0, separator), route.slice(separator + 1)];
}

/** Per-invocation transport controls. Retry safety is owned by the route. */
export type CallOptions = Omit<core.RequestOptions, "retryPolicy">;

export function call<R extends NoRequestCallRoute>(
  route: R,
): Promise<CallResult<R>>;
export function call<R extends NoRequestCallRoute>(
  route: R,
  request: undefined,
  options?: CallOptions,
): Promise<CallResult<R>>;
export function call<R extends RequestCallRoute>(
  route: R,
  request: CallRequest<R>,
  options?: CallOptions,
): Promise<CallResult<R>>;
export async function call(
  route: CallRoute,
  request?: unknown,
  options: CallOptions = {},
): Promise<unknown> {
  const [handler, command] = splitCallRoute(route);
  return core.request(handler, command, request ?? {}, {
    ...options,
    // Mutating Calls must never acquire a reconnect retry from a caller.
    retryPolicy: getRetryPolicy(route),
  });
}
