import { ReactNode } from "react";

import type { CapabilitiesResponse, CapabilityState } from "@/api/capabilities";

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
export interface AuthUser {
  /** Unique user identifier (typically a username or UID). */
  id: string;
  /** Friendly display name for the user. */
  name: string;
}

/**
 * Reducer-managed state representing the authentication context.
 */
export interface AuthState extends CapabilityState {
  isAuthenticated: boolean;
  isInitialized: boolean;
  privileged: boolean;
  user: AuthUser | null;
}

/**
 * The shape of the public API exposed by `useAuth()` or `AuthContext`.
 */
export interface AuthContextType extends CapabilityState {
  isAuthenticated: boolean;
  isInitialized: boolean;
  method: "session";
  privileged: boolean;
  refreshCapabilities: () => Promise<CapabilitiesResponse>;
  signIn: (username: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
  user: AuthUser | null;
}

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

  /** Dispatched when system capability checks are refreshed. */
  REFRESH_CAPABILITIES: "REFRESH_CAPABILITIES",

  /** Dispatched after logout or session expiration. */
  SIGN_OUT: "SIGN_OUT",
} as const satisfies Record<string, string>;

/**
 * Mapping between action types and their expected payloads.
 * Used to infer strong types for the reducer's action object.
 */
export interface AuthActionTypes {
  [AUTH_ACTIONS.INITIALIZE_FAILURE]: undefined;
  [AUTH_ACTIONS.INITIALIZE_START]: undefined;
  [AUTH_ACTIONS.INITIALIZE_SUCCESS]: {
    user: AuthUser;
    privileged: boolean;
  } & Partial<CapabilityState>;
  [AUTH_ACTIONS.REFRESH_CAPABILITIES]: Partial<CapabilityState>;
  [AUTH_ACTIONS.SIGN_IN]: {
    user: AuthUser;
    privileged: boolean;
  } & Partial<CapabilityState>;
  [AUTH_ACTIONS.SIGN_OUT]: undefined;
}

export type AuthActions = ActionMap<AuthActionTypes>[keyof AuthActionTypes];

export interface UpdateInfo {
  available: boolean;
  current_version: string;
  latest_version?: string;
  release_url?: string;
}

export interface LoginResponse extends CapabilitiesResponse {
  privileged: boolean;
  success: boolean;
  update?: UpdateInfo;
}

export type LoginErrorCode =
  | "invalid_request"
  | "session_creation_failed"
  | "authentication_failed"
  | "password_expired"
  | "access_denied"
  | "bridge_error"
  | "internal_error"
  | "login_failed";

export interface LoginErrorResponse {
  code?: LoginErrorCode;
  error?: string;
}
/**
 * Props accepted by the `<AuthProvider>` component.
 */
export interface AuthProviderProps {
  children: ReactNode;
}
