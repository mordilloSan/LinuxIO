/**
 * Builds the sign-in URL for the current location.
 *
 * When `preservePath` is set (e.g. an involuntary session loss), the current
 * path+query+hash is captured into a `redirect` query param so the user is
 * returned there after re-authenticating. This mirrors the param that the router
 * reads. Returns bare `/sign-in` for a deliberate
 * sign-out (`preservePath` false) or when already on the sign-in screen, so we
 * never bounce the user back into the app or self-redirect.
 */
export function buildSignInUrl(preservePath = false): string {
  const { pathname, search, hash } = window.location;

  if (!preservePath || pathname === "/sign-in") return "/sign-in";

  const target = `${pathname}${search}${hash}`;
  return `/sign-in?redirect=${encodeURIComponent(target)}`;
}

/**
 * Hard-navigate to the sign-in screen. The full reload also tears down all
 * in-memory app state (query cache, contexts, stream mux) — the intended reset
 * on session loss. See {@link buildSignInUrl} for how the target is chosen.
 */
export function redirectToSignIn(preservePath = false): void {
  window.location.assign(buildSignInUrl(preservePath));
}
