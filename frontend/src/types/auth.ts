import { ReactNode } from "react";

/**
 * Generic utility for creating discriminated union action types.
 * Used to define reducer-safe actions with optional payloads.
 */
export type ActionMap<M extends Record<string, any>> = {
  [Key in keyof M]: M[Key] extends undefined
    ? { type: Key }
    : { type: Key; payload: M[Key] };
};

/**
 * Represents an authenticated user in the session.
 */
export type AuthUser = {
  /** Unique user identifier (typically a username or UID). */
  id: string;
  /** Friendly display name for the user. */
  name: string;
};

/**
 * Reducer-managed state representing the authentication context.
 */
export type AuthState = {
  isAuthenticated: boolean;
  isInitialized: boolean;
  user: AuthUser | null;
};

/**
 * The shape of the public API exposed by `useAuth()` or `AuthContext`.
 */
export type AuthContextType = {
  isAuthenticated: boolean;
  isInitialized: boolean;
  user: AuthUser | null;
  method: "session";
  signIn: (username: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
};

/**
 * Enum-like constants for reducer action types.
 * These are used as discriminators in the auth reducer.
 */
export const AUTH_ACTIONS = {
  /** Dispatched when auth initialization begins (e.g., checking session). */
  INITIALIZE_START: "INITIALIZE_START",

  /** Dispatched when a valid session is found and the user is loaded. */
  INITIALIZE_SUCCESS: "INITIALIZE_SUCCESS",

  /** Dispatched when initialization fails (e.g., user not logged in). */
  INITIALIZE_FAILURE: "INITIALIZE_FAILURE",

  /** Dispatched after a successful login. */
  SIGN_IN: "SIGN_IN",

  /** Dispatched after logout or session expiration. */
  SIGN_OUT: "SIGN_OUT",
} as const satisfies Record<string, string>;

/**
 * Mapping between action types and their expected payloads.
 * Used to infer strong types for the reducer's action object.
 */
export type AuthActionTypes = {
  [AUTH_ACTIONS.INITIALIZE_START]: undefined;
  [AUTH_ACTIONS.INITIALIZE_SUCCESS]: { user: AuthUser };
  [AUTH_ACTIONS.INITIALIZE_FAILURE]: undefined;
  [AUTH_ACTIONS.SIGN_IN]: { user: AuthUser };
  [AUTH_ACTIONS.SIGN_OUT]: undefined;
};

export type AuthActions =
  ActionMap<AuthActionTypes>[keyof ActionMap<AuthActionTypes>];

/**
 * Props accepted by the `<AuthProvider>` component.
 */
export interface AuthProviderProps {
  children: ReactNode;
}
