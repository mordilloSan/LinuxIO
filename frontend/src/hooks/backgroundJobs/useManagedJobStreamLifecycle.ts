import { useCallback, useEffect, useRef, useState } from "react";

import { isTerminalJobState, type JobSnapshot, type Stream } from "@/api";

interface ManagedRun<TRequest> {
  detached: boolean;
  job: JobSnapshot | null;
  request: TRequest;
  stream: Stream | null;
}

/**
 * Page-owned lifecycle for one job-stream controller. Runs are matched by
 * request object identity, so callers must pass the same request object to
 * begin and every stream callback. This intentionally leaves React Query's
 * mutation contract untouched.
 */
export function useManagedJobStreamLifecycle<TRequest>(
  onCancel: (job: JobSnapshot, request: TRequest) => void,
) {
  const mountedRef = useRef(true);
  const runRef = useRef<ManagedRun<TRequest> | null>(null);
  const [isActive, setIsActive] = useState(false);

  const accepts = useCallback((request: TRequest) => {
    const run = runRef.current;
    return (
      !!run && run.request === request && !run.detached && mountedRef.current
    );
  }, []);

  const detachRun = useCallback((skipStateUpdate: boolean) => {
    const run = runRef.current;
    if (!run || run.detached) return false;
    run.detached = true;
    run.stream?.close();
    run.stream = null;
    if (!skipStateUpdate) setIsActive(false);
    return true;
  }, []);

  const detach = useCallback(() => detachRun(false), [detachRun]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      detachRun(true);
    };
  }, [detachRun]);

  const begin = useCallback((request: TRequest) => {
    const current = runRef.current;
    if (current && !current.detached) return false;
    runRef.current = { detached: false, job: null, request, stream: null };
    setIsActive(false);
    return true;
  }, []);

  const onJobStart = useCallback(
    (job: JobSnapshot, request: TRequest) => {
      if (!accepts(request)) return false;
      const run = runRef.current;
      if (!run) return false;
      run.job = job;
      setIsActive(!isTerminalJobState(job.state));
      return true;
    },
    [accepts],
  );

  const onOpen = useCallback(
    (stream: Stream, job: JobSnapshot, request: TRequest) => {
      const run = runRef.current;
      if (!accepts(request) || !run || run.job?.id !== job.id) {
        stream.close();
        return false;
      }
      run.stream = stream;
      return true;
    },
    [accepts],
  );

  const settle = useCallback(
    (request: TRequest) => {
      if (!accepts(request)) return false;
      runRef.current = null;
      setIsActive(false);
      return true;
    },
    [accepts],
  );

  const cancel = useCallback(() => {
    const run = runRef.current;
    if (!run || run.detached || !run.job || isTerminalJobState(run.job.state)) {
      return false;
    }
    run.detached = true;
    run.stream?.abort();
    run.stream = null;
    setIsActive(false);
    onCancel(run.job, run.request);
    return true;
  }, [onCancel]);

  return {
    accepts,
    begin,
    cancel,
    detach,
    isActive,
    onJobStart,
    onOpen,
    settle,
  };
}
