import { useContext } from "react";

import { AuthContext, type AuthContextType } from "@/contexts/AuthContext";

/**
 * Hook to access authentication state and methods.
 * Throws if used outside an AuthProvider.
 */
const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);

  if (!context)
    throw new Error("AuthContext must be placed within AuthProvider");

  return context;
};

export default useAuth;
