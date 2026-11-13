// Imported from the leaf module rather than "@/api": test files that mock the
// "@/api" barrel pull this helper in from inside that mock factory, and going
// through the barrel would re-enter it.
import { ROUTE_MODES } from "@/api/generated/route-metadata";

/**
 * Returns `api` with every Mutation endpoint's `useMutation(config)` calling
 * `useMutation({ route }, config)`, so a test answers a page's mutation hooks
 * from one mock keyed by route, the way it mocks `useCallMutation`. Other
 * endpoints, and anything else the endpoint carries, are left as they are.
 */
export function mockMutationHooks<T extends object>(
  api: T,
  // `never` takes a mock of any config type: the hook passes its caller's
  // config through untouched.
  useMutation: (endpoint: { route: string }, config: never) => unknown,
): T {
  const handlers = { ...api } as Record<string, unknown>;
  for (const [handler, commands] of Object.entries(handlers)) {
    if (typeof commands !== "object" || commands === null) continue;
    const endpoints: Record<string, unknown> = { ...commands };
    for (const [command, endpoint] of Object.entries(endpoints)) {
      const route = `${handler}.${command}`;
      if ((ROUTE_MODES as Record<string, string>)[route] !== "mutation") {
        continue;
      }
      const call =
        typeof endpoint === "function"
          ? (...args: unknown[]) =>
              (endpoint as (...a: unknown[]) => unknown)(...args)
          : {};
      endpoints[command] = Object.assign(call, endpoint, {
        route,
        useMutation: (config: unknown) =>
          useMutation({ route }, config as never),
      });
    }
    handlers[handler] = endpoints;
  }
  return handlers as T;
}
