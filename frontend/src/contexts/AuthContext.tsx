// src/contexts/AuthContext.tsx
import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useRef,
} from "react";
import { toast } from "sonner";

import {
  type CapabilitiesResponse,
  type CapabilityState,
  call,
  capabilitiesQueryKey,
  capabilityStateFromWire,
  closeStreamMux,
  emptyCapabilityState,
  initStreamMux,
  linuxio,
  type MuxStatus,
  parseCapabilityState,
  wireFromCapabilityState,
} from "@/api";
import type {
  AuthActions,
  AuthContextType,
  AuthProviderProps,
  AuthState,
  AuthUser,
  LoginErrorCode,
  LoginErrorResponse,
  LoginResponse,
} from "@/types/auth";
import { AUTH_ACTIONS } from "@/types/auth";
import { redirectToSignIn } from "@/utils/navigation";
import { setSigninNotice } from "@/utils/signinNotice";

const API_BASE = import.meta.env.VITE_API_URL || "";
const AUTH_CAPABILITIES_KEY = "auth_capabilities";
const LOGOUT_EVENT_KEY = "logout";
const SESSION_EXPIRED_EVENT_KEY = "session_expired";

type SignOutBroadcast =
  | typeof LOGOUT_EVENT_KEY
  | typeof SESSION_EXPIRED_EVENT_KEY
  | null;

const readStoredCapabilities = (): CapabilityState => {
  try {
    const raw = localStorage.getItem(AUTH_CAPABILITIES_KEY);
    if (!raw) return emptyCapabilityState;
    return parseCapabilityState(JSON.parse(raw));
  } catch {
    return emptyCapabilityState;
  }
};

const persistCapabilities = (capabilities: CapabilityState) => {
  localStorage.setItem(AUTH_CAPABILITIES_KEY, JSON.stringify(capabilities));
};

const loginErrorMessage = (
  code?: LoginErrorCode,
  fallback?: string,
): string => {
  switch (code) {
    case "invalid_request":
      return "The sign-in request was invalid. Refresh the page and try again.";
    case "session_creation_failed":
      return "LinuxIO could not prepare your session. Please try again.";
    case "authentication_failed":
      return "Incorrect username or password.";
    case "password_expired":
      return "Your password has expired. Change it in SSH or on the system console, then try again.";
    case "access_denied":
      return "This account is not allowed to sign in from the web interface.";
    case "bridge_error":
      return "LinuxIO authenticated you, but could not start the session bridge. Please try again.";
    case "internal_error":
      return "LinuxIO could not complete sign-in. Please try again.";
    default:
      return fallback || "Login failed";
  }
};

const initialState: AuthState = {
  isAuthenticated: false,
  isInitialized: false,
  user: null,
  privileged: false,
};

const reducer = (state: AuthState, action: AuthActions): AuthState => {
  switch (action.type) {
    case AUTH_ACTIONS.INITIALIZE_START:
      return { ...state, isInitialized: false };
    case AUTH_ACTIONS.INITIALIZE_SUCCESS:
      return {
        ...state,
        isInitialized: true,
        isAuthenticated: true,
        user: action.payload.user,
        privileged: action.payload.privileged,
      };
    case AUTH_ACTIONS.INITIALIZE_FAILURE:
      return {
        ...state,
        isInitialized: true,
        isAuthenticated: false,
        user: null,
        privileged: false,
      };
    case AUTH_ACTIONS.SIGN_IN:
      return {
        ...state,
        isAuthenticated: true,
        user: action.payload.user,
        privileged: action.payload.privileged,
      };
    case AUTH_ACTIONS.SIGN_OUT:
      return {
        ...state,
        isAuthenticated: false,
        user: null,
        privileged: false,
      };
    default: {
      const exhaustiveCheck: never = action;
      void exhaustiveCheck;
      return state;
    }
  }
};

const AuthContext = createContext<AuthContextType | null>(null);
AuthContext.displayName = "AuthContext";

function AuthProvider({ children }: AuthProviderProps) {
  const [state, dispatch] = useReducer(reducer, initialState);
  const queryClient = useQueryClient();
  const authGeneration = useRef(0);
  const userId = state.user?.id ?? "anonymous";

  // Unmounting invalidates the generation so a scan that settles afterwards
  // can no longer persist a snapshot.
  useEffect(() => {
    return () => {
      authGeneration.current += 1;
    };
  }, []);

  // Persist the last confirmed scan so the next page load can seed the cache
  // and gate capability-protected deep links before the mux reopens.
  const persistCapabilitySnapshot = useCallback(
    (wire: Partial<CapabilitiesResponse>) => {
      try {
        persistCapabilities(capabilityStateFromWire(wire));
      } catch (error) {
        console.error("Failed to store capability info:", error);
      }
    },
    [],
  );

  const refreshCapabilities =
    useCallback(async (): Promise<CapabilitiesResponse> => {
      const generation = authGeneration.current;
      const data = await call("system.get_capabilities");
      // A scan that settles after sign-out, unmount, or a newer sign-in must
      // not repopulate the cache or the stored snapshot.
      if (authGeneration.current === generation) {
        queryClient.setQueryData(capabilitiesQueryKey(userId), data);
        persistCapabilitySnapshot(data);
      }
      return data;
    }, [queryClient, userId, persistCapabilitySnapshot]);

  const initialize = useCallback(() => {
    dispatch({ type: AUTH_ACTIONS.INITIALIZE_START });

    // Check if we have stored user info from a previous session
    // The WebSocket connection will validate the session cookie
    const storedUsername = localStorage.getItem("auth_username");
    const storedPrivileged = localStorage.getItem("auth_privileged");

    if (storedUsername) {
      // Seed the capability cache from the last confirmed scan so route
      // guards can gate deep links before the mux reopens and rescans.
      const seed = wireFromCapabilityState(readStoredCapabilities());
      if (Object.keys(seed).length > 0) {
        queryClient.setQueryData(capabilitiesQueryKey(storedUsername), seed);
      }
      // Optimistically set authenticated - WebSocket will validate
      // If session is invalid, WebSocket will fail and trigger logout
      const user: AuthUser = { id: storedUsername, name: storedUsername };
      const privileged = storedPrivileged === "true";
      dispatch({
        type: AUTH_ACTIONS.INITIALIZE_SUCCESS,
        payload: { user, privileged },
      });
    } else {
      // No stored username, not authenticated
      dispatch({ type: AUTH_ACTIONS.INITIALIZE_FAILURE });
    }
  }, [queryClient]);

  // One place to clear local state and redirect.
  // `broadcast` writes the sign-out reason to localStorage so other tabs can
  // apply the matching redirect policy without rebroadcasting.
  // `preservePath` keeps the current location as a redirect target, used when
  // the session is lost involuntarily (not for deliberate sign-out).
  const doLocalSignOut = useCallback(
    (broadcast: SignOutBroadcast, preservePath = false) => {
      authGeneration.current += 1;
      queryClient.removeQueries({
        queryKey: linuxio.system.get_capabilities.queryKey,
      });
      // Clear update info and user data on logout
      try {
        sessionStorage.removeItem("update_info");
        sessionStorage.removeItem("update_info_checked");
        localStorage.removeItem("auth_username");
        localStorage.removeItem("auth_privileged");
        localStorage.removeItem(AUTH_CAPABILITIES_KEY);
      } catch {
        /* ignore */
      }
      if (broadcast) {
        try {
          localStorage.setItem(broadcast, String(Date.now()));
        } catch {
          /* ignore */
        }
      }
      dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
      redirectToSignIn(preservePath);
    },
    [queryClient],
  );

  // The session was lost involuntarily (expired/invalidated, not a deliberate
  // sign-out): tear down locally without calling the logout endpoint, preserve
  // the current path, and leave a one-shot notice for the sign-in screen. A
  // toast would be lost to the redirect (and the sign-in screen has no Toaster).
  const sessionExpired = useCallback(() => {
    setSigninNotice("expired");
    doLocalSignOut(SESSION_EXPIRED_EVENT_KEY, true);
  }, [doLocalSignOut]);

  // Init on mount
  useEffect(() => {
    initialize();
  }, [initialize]);

  // Cross-tab logout via localStorage
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      // Ignore key removals; only newly published auth events are actionable.
      if (e.newValue === null) return;

      if (e.key === LOGOUT_EVENT_KEY) {
        // other tab asked us to logout; do not rebroadcast
        doLocalSignOut(null);
      } else if (e.key === SESSION_EXPIRED_EVENT_KEY) {
        // Preserve this tab's own path and surface the expiry notice here too.
        setSigninNotice("expired");
        doLocalSignOut(null, true);
      }
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [doLocalSignOut]);

  // Initialize stream multiplexer when authenticated
  // WebSocket connection validates session - if invalid, triggers logout
  useEffect(() => {
    if (state.isAuthenticated) {
      const scopedKey = capabilitiesQueryKey(state.user?.id ?? "anonymous");
      const generation = authGeneration.current;
      const mux = initStreamMux();

      const warmCapabilities = () => {
        // The query cache dedupes concurrent open notifications and keeps the
        // settled scan for the whole sign-in (staleTime: Infinity); a failed
        // scan leaves no entry, so a later open retries it.
        void queryClient
          .query({
            ...linuxio.system.get_capabilities,
            queryKey: scopedKey,
            staleTime: Number.POSITIVE_INFINITY,
            meta: { silent: true },
          })
          .then((data) => {
            if (authGeneration.current === generation) {
              persistCapabilitySnapshot(data);
            }
          })
          .catch(() => {
            // A failed scan may be retried after a later reconnect/open event.
          });
      };

      // Listen for WebSocket status changes
      const unsubscribe = mux.addStatusListener((status: MuxStatus) => {
        if (status === "open") {
          warmCapabilities();
        } else if (status === "error") {
          // "error" status means close code 1008 (session expired/invalid)
          // or WebSocket connection failed (session cookie invalid)
          console.log("[AuthContext] Session invalid or expired");
          sessionExpired();
        } else if (status === "closed") {
          // Network issue or tab closed - don't logout
          // Session cookie might still be valid
          console.log(
            "[AuthContext] WebSocket closed (network issue or tab closed)",
          );
          // Don't logout - StreamMultiplexer will auto-reconnect
        }
      });
      if (mux.status === "open") warmCapabilities();
      return () => {
        unsubscribe();
      };
    } else {
      closeStreamMux();
    }
  }, [
    state.isAuthenticated,
    state.user?.id,
    queryClient,
    persistCapabilitySnapshot,
    sessionExpired,
  ]);

  const signIn = useCallback(
    async (username: string, password: string) => {
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: "POST",
        credentials: "include",
        cache: "no-store",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
      if (!res.ok) {
        const err = (await res.json().catch(() => ({}))) as LoginErrorResponse;
        throw new Error(loginErrorMessage(err.code, err.error));
      }
      const data: LoginResponse = await res.json();

      // Store username and privileged status in localStorage (persists across tab close)
      try {
        localStorage.setItem("auth_username", username);
        localStorage.setItem("auth_privileged", String(data.privileged));
        localStorage.removeItem(AUTH_CAPABILITIES_KEY);
      } catch (error) {
        console.error("Failed to store user info:", error);
      }

      const user: AuthUser = { id: username, name: username };
      authGeneration.current += 1;
      // A fresh sign-in must not inherit an earlier session's scan.
      queryClient.removeQueries({
        queryKey: linuxio.system.get_capabilities.queryKey,
      });
      dispatch({
        type: AUTH_ACTIONS.SIGN_IN,
        payload: {
          user,
          privileged: data.privileged,
        },
      });

      // Show welcome message
      toast.success(`Welcome, ${username}!`);
    },
    [queryClient],
  );

  const signOut = useCallback(async () => {
    try {
      await fetch(`${API_BASE}/auth/logout`, {
        method: "GET",
        credentials: "include",
        cache: "no-store",
      });
    } catch {
      // ignore; we still want to clear locally
    }
    doLocalSignOut(LOGOUT_EVENT_KEY);
  }, [doLocalSignOut]);

  const contextValue = useMemo(
    () => ({
      ...state,
      method: "session" as const,
      signIn,
      signOut,
      sessionExpired,
      refreshCapabilities,
    }),
    [state, signIn, signOut, sessionExpired, refreshCapabilities],
  );

  return (
    <AuthContext.Provider value={contextValue}>{children}</AuthContext.Provider>
  );
}

export { AuthContext, AuthProvider };
