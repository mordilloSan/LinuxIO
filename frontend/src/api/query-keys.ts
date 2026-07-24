import type { QueryKey } from "@tanstack/react-query";

export type RequestShape =
  | { kind: "none" }
  | { kind: "object" }
  | { kind: "field"; field: string };

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function requestForWire(
  requestShape: RequestShape,
  request: unknown,
): unknown {
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

export function endpointQueryKey(
  handler: string,
  command: string,
  requestShape: RequestShape,
  request?: unknown,
): QueryKey {
  if (requestShape.kind === "none") {
    return ["linuxio", handler, command];
  }
  return ["linuxio", handler, command, requestForWire(requestShape, request)];
}

export function endpointQueryPrefix(route: string): QueryKey {
  const separator = route.indexOf(".");
  if (separator <= 0 || separator === route.length - 1) {
    throw new Error(`Invalid API route: ${route}`);
  }
  return ["linuxio", route.slice(0, separator), route.slice(separator + 1)];
}

export function handlerQueryPrefix(handler: string): QueryKey {
  return ["linuxio", handler];
}
