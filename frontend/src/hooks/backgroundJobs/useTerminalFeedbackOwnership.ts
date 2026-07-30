import { useCallback, useEffect, useRef } from "react";

import {
  claimTerminalFeedback,
  markTerminalFeedbackEmitted,
} from "./terminalJobFeedback";

/**
 * Keeps page-owned terminal-feedback claims paired with the lifecycle that
 * owns them. Consumers only declare claim/mark/release points; unmount always
 * returns ownership to the global fallback.
 */
export function useTerminalFeedbackOwnership(type: string) {
  const jobIdRef = useRef<string | null>(null);
  const releaseRef = useRef<(() => void) | null>(null);

  const claim = (jobId?: string) => {
    releaseRef.current ??= claimTerminalFeedback(type);
    if (jobId) jobIdRef.current = jobId;
  };

  const mark = useCallback(() => {
    if (jobIdRef.current) markTerminalFeedbackEmitted(jobIdRef.current);
  }, []);

  const release = useCallback(() => {
    releaseRef.current?.();
    releaseRef.current = null;
    jobIdRef.current = null;
  }, []);

  useEffect(() => release, [release]);

  return { claim, mark, release };
}
