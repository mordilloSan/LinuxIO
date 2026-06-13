import { describe, expect, it } from "vitest";

import { AuthContext } from "@/contexts/AuthContext";
import useAuth from "@/hooks/useAuth";
import { createAuthContextValue, renderHook } from "@/test/render";

describe("useAuth", () => {
  it("throws when used outside AuthProvider", () => {
    expect(() => renderHook(() => useAuth())).toThrow(
      "AuthContext must be placed within AuthProvider",
    );
  });

  it("returns the current auth context", () => {
    const value = createAuthContextValue({
      isAuthenticated: true,
      user: { id: "miguel", name: "Miguel" },
    });

    const { result } = renderHook(() => useAuth(), {
      wrapper: ({ children }) => (
        <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
      ),
    });

    expect(result.current.isAuthenticated).toBe(true);
    expect(result.current.user?.name).toBe("Miguel");
  });
});
