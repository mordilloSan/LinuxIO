import type {
  CallRequest,
  CallResult,
  CallRoute,
  NoRequestCallRoute,
  RequestCallRoute,
} from "./generated/linuxio-types";
import * as core from "./linuxio-core";

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

export function getRetryPolicy(
  handler: string,
  command: string,
): core.RequestOptions["retryPolicy"] {
  const route = `${handler}.${command}`;
  if (
    RETRYABLE_COMMAND_PREFIXES.some((prefix) => command.startsWith(prefix)) ||
    RETRYABLE_COMMANDS.has(route)
  ) {
    return "connection_closed";
  }
  return "none";
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
    retryPolicy: getRetryPolicy(handler, command),
  });
}
