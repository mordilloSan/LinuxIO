// src/contexts/AuthContext.tsx
import {
  createContext,
  useEffect,
  useReducer,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import { initStreamMux, closeStreamMux, MuxStatus } from "@/api/linuxio";
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

  const initialize = useCallback(() => {
    dispatch({ type: AUTH_ACTIONS.INITIALIZE_START });

    // Try to retrieve stored user from sessionStorage
    const storedUsername = sessionStorage.getItem("auth_username");
    if (!storedUsername) {
      // No stored session, mark as initialized but not authenticated
      dispatch({ type: AUTH_ACTIONS.INITIALIZE_FAILURE });
      return;
    }

    // Initialize WebSocket - if session is valid, it will connect successfully
    const mux = initStreamMux();

    // Listen for connection status to determine if session is valid
    const unsubscribe = mux.addStatusListener((status: MuxStatus) => {
      if (status === "open") {
        // WebSocket connected successfully - session is valid
        const user: AuthUser = { id: storedUsername, name: storedUsername };
        dispatch({ type: AUTH_ACTIONS.INITIALIZE_SUCCESS, payload: { user } });
        unsubscribe();
      } else if (status === "error" || status === "closed") {
        // WebSocket failed to connect - session is invalid
        sessionStorage.removeItem("auth_username");
        dispatch({ type: AUTH_ACTIONS.INITIALIZE_FAILURE });
        unsubscribe();
      }
    });
  }, []);

  // One place to clear local state and redirect.
  // `broadcast` writes to localStorage so other tabs receive it.
  const doLocalSignOut = useCallback((broadcast: boolean) => {
    // Clear update info and username on logout
    try {
      sessionStorage.removeItem("update_info");
      sessionStorage.removeItem("auth_username");
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
  // Also listen for unexpected WebSocket closure to handle session expiry
  useEffect(() => {
    if (state.isAuthenticated) {
      const mux = initStreamMux();
      // Listen for WebSocket closure - indicates session expiry
      const unsubscribe = mux.addStatusListener((status: MuxStatus) => {
        if (status === "closed" || status === "error") {
          console.log("[AuthContext] Stream mux closed/error, session expired");
          toast.error("Session expired. Please sign in again.");
          doLocalSignOut(false);
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

    setIndexerAvailabilityFlag(data.indexer_available ?? null);

    // Store username and create user object
    try {
      sessionStorage.setItem("auth_username", username);
    } catch (error) {
      console.error("Failed to store username:", error);
    }

    const user: AuthUser = { id: username, name: username };
    dispatch({ type: AUTH_ACTIONS.SIGN_IN, payload: { user } });

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
