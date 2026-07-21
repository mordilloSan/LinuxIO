/**
 * Hard-navigate to the sign-in screen.
 *
 * When `preservePath` is set (e.g. an involuntary session loss), the current
 * location is captured into a `redirect` query param so the user is returned
 * there after re-authenticating. This mirrors the param that AuthGuard and
 * GuestGuard already read; deliberate sign-out passes `false` so logging out
 * doesn't bounce the user back into the app.
 */
export function redirectToSignIn(preservePath = false): void {
  const { pathname, search, hash } = window.location;

  if (!preservePath || pathname === "/sign-in") {
    window.location.assign("/sign-in");
    return;
  }

  const target = `${pathname}${search}${hash}`;
  window.location.assign(`/sign-in?redirect=${encodeURIComponent(target)}`);
}
