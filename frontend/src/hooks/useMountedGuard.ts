import { useCallback, useEffect, useRef } from "react";

/** Returns whether the owning component is still mounted without leaking refs to callers. */
export function useMountedGuard(): () => boolean {
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  return useCallback(() => mountedRef.current, []);
}
