// src/contexts/AuthContext.tsx
import {
  createContext,
  useEffect,
  useReducer,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import {
  linuxio,
  initStreamMux,
  closeStreamMux,
  type MuxStatus,
  type CapabilitiesResponse,
} from "@/api";
import {
  AuthContextType,
  AuthState,
  AuthActions,
  AuthProviderProps,
  AUTH_ACTIONS,
  AuthUser,
  LoginResponse,
} from "@/types/auth";

const API_BASE = import.meta.env.VITE_API_URL || "";

const initialState: AuthState = {
  isAuthenticated: false,
  isInitialized: false,
  user: null,
  privileged: false,
  dockerAvailable: null,
  indexerAvailable: null,
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
        dockerAvailable: action.payload.dockerAvailable ?? null,
        indexerAvailable: action.payload.indexerAvailable ?? null,
      };
    case AUTH_ACTIONS.INITIALIZE_FAILURE:
      return {
        ...state,
        isInitialized: true,
        isAuthenticated: false,
        user: null,
        privileged: false,
        dockerAvailable: null,
        indexerAvailable: null,
      };
    case AUTH_ACTIONS.SIGN_IN:
      return {
        ...state,
        isAuthenticated: true,
        user: action.payload.user,
        privileged: action.payload.privileged,
        dockerAvailable: action.payload.dockerAvailable ?? null,
        indexerAvailable: action.payload.indexerAvailable ?? null,
      };
    case AUTH_ACTIONS.SIGN_OUT:
      return {
        ...state,
        isAuthenticated: false,
        user: null,
        privileged: false,
        dockerAvailable: null,
        indexerAvailable: null,
      };
    case AUTH_ACTIONS.UPDATE_CAPABILITIES:
      return {
        ...state,
        dockerAvailable: action.payload.dockerAvailable,
        indexerAvailable: action.payload.indexerAvailable,
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

  const initialize = useCallback(async () => {
    dispatch({ type: AUTH_ACTIONS.INITIALIZE_START });

    // Check if we have stored user info from a previous session
    // The WebSocket connection will validate the session cookie
    const storedUsername = localStorage.getItem("auth_username");
    const storedPrivileged = localStorage.getItem("auth_privileged");

    if (storedUsername) {
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
  }, []);

  // One place to clear local state and redirect.
  // `broadcast` writes to localStorage so other tabs receive it.
  const doLocalSignOut = useCallback((broadcast: boolean) => {
    // Clear update info and user data on logout
    try {
      sessionStorage.removeItem("update_info");
      localStorage.removeItem("auth_username");
      localStorage.removeItem("auth_privileged");
    } catch {
      /* ignore */
    }
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

  // Init on mount
  useEffect(() => {
    initialize();
  }, [initialize]);

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
  // WebSocket connection validates session - if invalid, triggers logout
  useEffect(() => {
    if (state.isAuthenticated) {
      const mux = initStreamMux();

      const refreshCapabilities = async () => {
        try {
          const caps = await linuxio.call<CapabilitiesResponse>(
            "system",
            "get_capabilities",
          );
          dispatch({
            type: AUTH_ACTIONS.UPDATE_CAPABILITIES,
            payload: {
              dockerAvailable: caps.docker_available,
              indexerAvailable: caps.indexer_available,
            },
          });
        } catch (err) {
          console.warn("[AuthContext] Failed to refresh capabilities:", err);
        }
      };

      if (mux.status === "open") {
        void refreshCapabilities();
      }
      // Listen for WebSocket status changes
      const unsubscribe = mux.addStatusListener((status: MuxStatus) => {
        if (status === "error") {
          // "error" status means close code 1008 (session expired/invalid)
          // or WebSocket connection failed (session cookie invalid)
          console.log("[AuthContext] Session invalid or expired");
          toast.error("Session expired. Please sign in again.");
          doLocalSignOut(false);
        } else if (status === "open") {
          void refreshCapabilities();
        } else if (status === "closed") {
          // Network issue or tab closed - don't logout
          // Session cookie might still be valid
          console.log(
            "[AuthContext] WebSocket closed (network issue or tab closed)",
          );
          // Don't logout - user can refresh to reconnect
        }
      });
      return () => unsubscribe();
    } else {
      closeStreamMux();
    }
  }, [state.isAuthenticated, doLocalSignOut]);

  const signIn = useCallback(async (username: string, password: string) => {
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

    // Store username and privileged status in localStorage (persists across tab close)
    try {
      localStorage.setItem("auth_username", username);
      localStorage.setItem("auth_privileged", String(data.privileged));
    } catch (error) {
      console.error("Failed to store user info:", error);
    }

    const user: AuthUser = { id: username, name: username };
    dispatch({
      type: AUTH_ACTIONS.SIGN_IN,
      payload: {
        user,
        privileged: data.privileged,
      },
    });

    // Show welcome message
    toast.success(`Welcome, ${username}!`);
  }, []);

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
