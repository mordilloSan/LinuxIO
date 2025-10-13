// src/contexts/AuthContext.tsx
import React, {
  createContext,
  useEffect,
  useReducer,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import useSessionChecker from "@/hooks/useSessionChecker";
import {
  AuthContextType,
  AuthState,
  AuthActions,
  AuthProviderProps,
  AUTH_ACTIONS,
  AuthUser,
} from "@/types/auth";
import axios from "@/utils/axios";
import { resetFilebrowserUserCache } from "@/utils/filebrowser";

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
    const { data } = await axios.get<{ user: AuthUser }>("/auth/me");
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
    try {
      resetFilebrowserUserCache();
    } catch {
      /* ignore */
    }
    // Clear update info on logout
    try {
      sessionStorage.removeItem("update_info");
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

  const signIn = useCallback(
    async (username: string, password: string) => {
      // Login response may include update info
      const { data } = await axios.post<{
        success: boolean;
        privileged: boolean;
        update?: {
          available: boolean;
          current_version: string;
          latest_version?: string;
          release_url?: string;
        };
      }>("/auth/login", { username, password });

      // Store update info if present
      if (data.update) {
        try {
          sessionStorage.setItem("update_info", JSON.stringify(data.update));
        } catch (error) {
          console.error("Failed to store update info:", error);
        }
      }

      const user = await fetchUser();
      dispatch({ type: AUTH_ACTIONS.SIGN_IN, payload: { user } });
      toast.success(`Welcome, ${username}!`);
    },
    [fetchUser],
  );

  const signOut = useCallback(async () => {
    try {
      await axios.get("/auth/logout");
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
