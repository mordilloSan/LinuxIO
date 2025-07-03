import { useCallback } from "react";
import { AuthUser, AUTH_ACTIONS, AuthState, AuthActions } from "@/types/auth";

function isSameUser(u1: AuthUser | null, u2: AuthUser | null): boolean {
  if (!u1 || !u2) return false;
  return u1.id === u2.id;
}

/**
 * Hook to create a session checker function.
 *
 * It fetches the user and only dispatches state changes if necessary.
 *
 * @param fetchUser - async function returning AuthUser
 * @param state - current AuthState
 * @param dispatch - reducer dispatch function
 * @param options - optional callbacks and logging
 */
export default function useSessionChecker(
  fetchUser: () => Promise<AuthUser>,
  state: AuthState,
  dispatch: React.Dispatch<AuthActions>,
  options?: {
    onSignOut?: () => void;
    onSignIn?: (user: AuthUser) => void;
    log?: boolean;
  },
) {
  const checkSession = useCallback(async () => {
    try {
      const user = await fetchUser();

      if (state.isAuthenticated) {
        if (isSameUser(state.user, user)) {
          if (options?.log) console.log("[SessionChecker] Session unchanged.");
          return;
        }
        if (options?.log)
          console.log("[SessionChecker] User changed, updating.");
      } else {
        if (options?.log)
          console.log("[SessionChecker] Was unauthenticated, now signed in.");
      }

      dispatch({ type: AUTH_ACTIONS.SIGN_IN, payload: { user } });
      options?.onSignIn?.(user);
    } catch {
      if (!state.isAuthenticated) {
        if (options?.log)
          console.log("[SessionChecker] Still unauthenticated, skipping.");
        return;
      }

      if (options?.log)
        console.log("[SessionChecker] Session expired, signing out.");

      dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
      options?.onSignOut?.();
    }
  }, [fetchUser, state.isAuthenticated, state.user, dispatch, options]);

  return checkSession;
}
