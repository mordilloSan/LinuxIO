// src/contexts/AuthContext.tsx
import {
  createContext,
  useEffect,
  useReducer,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import { initStreamMux, closeStreamMux } from "@/api/linuxio";
import useSessionChecker from "@/hooks/useSessionChecker";
import {
  AuthContextType,
  AuthState,
  AuthActions,
  AuthProviderProps,
  AUTH_ACTIONS,
  AuthUser,
  LoginResponse,
} from "@/types/auth";
import {
  clearIndexerAvailabilityFlag,
  setIndexerAvailabilityFlag,
} from "@/utils/indexerAvailability";

const API_BASE = import.meta.env.VITE_API_URL || "";

const initialState: AuthState = {
  isAuthenticated: false,
  isInitialized: false,
  user: null,
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
      };
    case AUTH_ACTIONS.INITIALIZE_FAILURE:
      return {
        ...state,
        isInitialized: true,
        isAuthenticated: false,
        user: null,
      };
    case AUTH_ACTIONS.SIGN_IN:
      return { ...state, isAuthenticated: true, user: action.payload.user };
    case AUTH_ACTIONS.SIGN_OUT:
      return { ...state, isAuthenticated: false, user: null };
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

  const fetchUser = useCallback(async (): Promise<AuthUser> => {
    const res = await fetch(`${API_BASE}/auth/me`, { credentials: "include" });
    if (!res.ok) throw new Error("Failed to fetch user");
    const data: { user: AuthUser } = await res.json();
    return data.user;
  }, []);

  const initialize = useCallback(async () => {
    dispatch({ type: AUTH_ACTIONS.INITIALIZE_START });
    try {
      const user = await fetchUser();
      dispatch({ type: AUTH_ACTIONS.INITIALIZE_SUCCESS, payload: { user } });
    } catch {
      dispatch({ type: AUTH_ACTIONS.INITIALIZE_FAILURE });
    }
  }, [fetchUser]);

  // One place to clear local state and redirect.
  // `broadcast` writes to localStorage so other tabs receive it.
  const doLocalSignOut = useCallback((broadcast: boolean) => {
    // Clear update info on logout
    try {
      sessionStorage.removeItem("update_info");
    } catch {
      /* ignore */
    }
    clearIndexerAvailabilityFlag();
    if (broadcast) {
      try {
        localStorage.setItem("logout", String(Date.now()));
      } catch {
        /* ignore */
      }
    }
    dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
    // Use react-router navigate if available; otherwise:
    window.location.assign("/sign-in");
  }, []);

  const checkSession = useSessionChecker(fetchUser, state, dispatch, {
    onSignOut: () => {
      toast.error("Session expired. Please sign in again.");
      doLocalSignOut(false);
    },
    log: true,
  });

  // Init on mount
  useEffect(() => {
    initialize();
  }, [initialize]);

  // Subscribe to visibility/focus to re-check session
  useEffect(() => {
    if (!state.isInitialized) return;
    const handle = () => {
      if (document.visibilityState === "visible") checkSession();
    };
    window.addEventListener("visibilitychange", handle);
    window.addEventListener("focus", handle);
    return () => {
      window.removeEventListener("visibilitychange", handle);
      window.removeEventListener("focus", handle);
    };
  }, [checkSession, state.isInitialized]);

  // Periodic session check while authenticated (every 1 minute for testing, increase in production)
  // This detects session expiry even if user is just watching the dashboard
  useEffect(() => {
    if (!state.isInitialized || !state.isAuthenticated) return;

    const interval = setInterval(
      () => {
        // Only check if tab is visible (don't waste resources in background)
        if (document.visibilityState === "visible") {
          checkSession();
        }
      },
      5 * 60 * 1000,
    );

    return () => clearInterval(interval);
  }, [checkSession, state.isInitialized, state.isAuthenticated]);

  // Cross-tab logout via localStorage
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === "logout") {
        // other tab asked us to logout; do not rebroadcast
        doLocalSignOut(false);
      }
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [doLocalSignOut]);

  // Initialize stream multiplexer when authenticated
  // Also listen for unexpected WebSocket closure to trigger session check
  useEffect(() => {
    if (state.isAuthenticated) {
      const mux = initStreamMux();
      // Listen for WebSocket closure - could indicate session expiry
      const unsubscribe = mux.addStatusListener((status) => {
        if (status === "closed" || status === "error") {
          console.log(
            "[AuthContext] Stream mux closed/error, checking session...",
          );
          checkSession();
        }
      });
      return () => unsubscribe();
    } else {
      closeStreamMux();
    }
  }, [state.isAuthenticated, checkSession]);

  const signIn = useCallback(
    async (username: string, password: string) => {
      // Login response may include update info
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.error || "Login failed");
      }
      const data: LoginResponse = await res.json();

      // Store update info if present
      if (data.update) {
        try {
          sessionStorage.setItem("update_info", JSON.stringify(data.update));
        } catch (error) {
          console.error("Failed to store update info:", error);
        }
      }

      setIndexerAvailabilityFlag(data.indexer_available ?? null);

      const user = await fetchUser();
      dispatch({ type: AUTH_ACTIONS.SIGN_IN, payload: { user } });

      // Show welcome message
      toast.success(`Welcome, ${username}!`);
    },
    [fetchUser],
  );

  const signOut = useCallback(async () => {
    try {
      await fetch(`${API_BASE}/auth/logout`, { credentials: "include" });
    } catch {
      // ignore; we still want to clear locally
    }
    doLocalSignOut(true);
  }, [doLocalSignOut]);

  const contextValue = useMemo(
    () => ({
      ...state,
      method: "session" as const,
      signIn,
      signOut,
    }),
    [state, signIn, signOut],
  );

  return (
    <AuthContext.Provider value={contextValue}>{children}</AuthContext.Provider>
  );
}

export { AuthContext, AuthProvider };
