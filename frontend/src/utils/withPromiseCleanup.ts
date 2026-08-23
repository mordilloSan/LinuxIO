/**
 * Runs synchronous cleanup after a promise settles.
 *
 * Promise.finally preserves the original value or error unless cleanup throws,
 * in which case the cleanup error becomes the result.
 */
export function withPromiseCleanup<TResult>(
  promise: Promise<TResult>,
  cleanup: () => void,
): Promise<TResult> {
  return promise.finally(cleanup);
}
