import { useEffect, useEffectEvent, useRef } from "react";

/**
 * Registers a parent-owned "create" action handler exactly once on mount.
 *
 * List/tab pages expose their create action (e.g. `handleCreateUser`) to a parent
 * toolbar via a `register` prop. Doing this in a plain effect with
 * `[register, handler]` deps re-runs on every parent render, because both are
 * usually fresh identities (inline arrows / `useCallback`). This hook registers
 * a stable wrapper once: the wrapper always calls the latest `handler` (via a
 * ref), and the one-time registration reads `register` non-reactively through
 * `useEffectEvent`, so there are no needless re-registrations.
 *
 * @param register Parent callback that stores the child's create handler.
 * @param handler  The child's current create action.
 */
export function useRegisterCreateHandler(
  register: ((handler: () => void) => void) | undefined,
  handler: () => void,
): void {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  const registerOnMount = useEffectEvent(() => {
    register?.(() => handlerRef.current());
  });

  useEffect(() => {
    registerOnMount();
  }, []);
}
