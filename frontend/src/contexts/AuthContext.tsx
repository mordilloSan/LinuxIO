// src/contexts/AuthContext.tsx
import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useReducer,
} from "react";
import { toast } from "sonner";

import {
  type CapabilitiesResponse,
  type CapabilityState,
  capabilityStateFromWire,
  closeStreamMux,
  emptyCapabilityState,
  initStreamMux,
  linuxio,
  type MuxStatus,
  parseCapabilityState,
  pickCapabilityState,
} from "@/api";
import {
  AUTH_ACTIONS,
  AuthActions,
  AuthContextType,
  AuthProviderProps,
  AuthState,
  AuthUser,
  LoginErrorCode,
  LoginErrorResponse,
  LoginResponse,
} from "@/types/auth";
import { clearConfigCache } from "@/utils/configCache";

const API_BASE = import.meta.env.VITE_API_URL || "";
const AUTH_CAPABILITIES_KEY = "auth_capabilities";

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
  ...emptyCapabilityState,
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
        ...pickCapabilityState(action.payload),
      };
    case AUTH_ACTIONS.INITIALIZE_FAILURE:
      return {
        ...state,
        isInitialized: true,
        isAuthenticated: false,
        user: null,
        privileged: false,
        ...emptyCapabilityState,
      };
    case AUTH_ACTIONS.SIGN_IN:
      return {
        ...state,
        isAuthenticated: true,
        user: action.payload.user,
        privileged: action.payload.privileged,
        ...pickCapabilityState(action.payload),
      };
    case AUTH_ACTIONS.REFRESH_CAPABILITIES:
      return {
        ...state,
        ...pickCapabilityState(action.payload),
      };
    case AUTH_ACTIONS.SIGN_OUT:
      return {
        ...state,
        isAuthenticated: false,
        user: null,
        privileged: false,
        ...emptyCapabilityState,
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

  const applyCapabilities = useCallback(
    (data: Partial<CapabilitiesResponse>) => {
      const capabilities = capabilityStateFromWire(data);
      try {
        persistCapabilities(capabilities);
      } catch (error) {
        console.error("Failed to store capability info:", error);
      }
      dispatch({
        type: AUTH_ACTIONS.REFRESH_CAPABILITIES,
        payload: capabilities,
      });
      return capabilities;
    },
    [],
  );

  const refreshCapabilities =
    useCallback(async (): Promise<CapabilitiesResponse> => {
      const data = await linuxio.system.get_capabilities();
      applyCapabilities(data);
      return data;
    }, [applyCapabilities]);

  const initialize = useCallback(async () => {
    dispatch({ type: AUTH_ACTIONS.INITIALIZE_START });

    // Check if we have stored user info from a previous session
    // The WebSocket connection will validate the session cookie
    const storedUsername = localStorage.getItem("auth_username");
    const storedPrivileged = localStorage.getItem("auth_privileged");
    const storedCapabilities = readStoredCapabilities();

    if (storedUsername) {
      // Optimistically set authenticated - WebSocket will validate
      // If session is invalid, WebSocket will fail and trigger logout
      const user: AuthUser = { id: storedUsername, name: storedUsername };
      const privileged = storedPrivileged === "true";
      dispatch({
        type: AUTH_ACTIONS.INITIALIZE_SUCCESS,
        payload: { user, privileged, ...storedCapabilities },
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
      clearConfigCache();
      localStorage.removeItem("auth_username");
      localStorage.removeItem("auth_privileged");
      localStorage.removeItem(AUTH_CAPABILITIES_KEY);
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
      // Listen for WebSocket status changes
      const unsubscribe = mux.addStatusListener((status: MuxStatus) => {
        if (status === "error") {
          // "error" status means close code 1008 (session expired/invalid)
          // or WebSocket connection failed (session cookie invalid)
          console.log("[AuthContext] Session invalid or expired");
          toast.error("Session expired. Please sign in again.");
          doLocalSignOut(false);
        } else if (status === "closed") {
          // Network issue or tab closed - don't logout
          // Session cookie might still be valid
          console.log(
            "[AuthContext] WebSocket closed (network issue or tab closed)",
          );
          // Don't logout - StreamMultiplexer will auto-reconnect
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
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    if (!res.ok) {
      const err = (await res.json().catch(() => ({}))) as LoginErrorResponse;
      throw new Error(loginErrorMessage(err.code, err.error));
    }
    const data: LoginResponse = await res.json();
    const capabilities = capabilityStateFromWire(data);

    clearConfigCache();

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
      persistCapabilities(capabilities);
    } catch (error) {
      console.error("Failed to store user info:", error);
    }

    const user: AuthUser = { id: username, name: username };
    dispatch({
      type: AUTH_ACTIONS.SIGN_IN,
      payload: {
        user,
        privileged: data.privileged,
        ...capabilities,
      },
    });

    // Show welcome message
    toast.success(`Welcome, ${username}!`);
  }, []);

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
    doLocalSignOut(true);
  }, [doLocalSignOut]);

  const contextValue = useMemo(
    () => ({
      ...state,
      method: "session" as const,
      signIn,
      signOut,
      refreshCapabilities,
    }),
    [state, signIn, signOut, refreshCapabilities],
  );

  return (
    <AuthContext.Provider value={contextValue}>{children}</AuthContext.Provider>
  );
}

export { AuthContext, AuthProvider };
