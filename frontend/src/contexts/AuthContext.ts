import { createContext } from "react";

/** An authenticated user in the session. */
export interface AuthUser {
  /** Unique user identifier (typically a username or UID). */
  id: string;
  /** Friendly display name for the user. */
  name: string;
}

/**
 * Reducer-managed authentication state. Capability flags live in the query
 * cache (see useCapabilityState), not here.
 */
export interface AuthState {
  isAuthenticated: boolean;
  isInitialized: boolean;
  privileged: boolean;
  user: AuthUser | null;
}

/** The public API exposed by `useAuth()`. */
export interface AuthContextType extends AuthState {
  method: "session";
  refreshCapabilities: () => Promise<unknown>;
  /** Tear down locally after an involuntary session loss, preserving the path. */
  sessionExpired: () => void;
  signIn: (username: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextType | null>(null);
AuthContext.displayName = "AuthContext";
