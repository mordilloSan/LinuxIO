import {
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";

import { HeaderActionSlotContext } from "@/contexts/HeaderActionSlotContext";

/**
 * Lends the app header's corner to whichever route header is on screen. On
 * small screens a route condenses its icons behind a single trigger; that
 * trigger belongs in the one place a header control always lives, rather than
 * in a per-route bar that scrolls or changes shape between routes.
 */
export const HeaderActionSlotProvider = ({ children }: PropsWithChildren) => {
  // Kept for the provider's lifetime so opening the menu does not tear down
  // the state its trigger holds when the route re-renders.
  const [host] = useState(() => {
    const element = document.createElement("div");
    element.className = "app-navbar__route-actions-portal";
    return element;
  });
  const [mountedHost, setMountedHost] = useState<HTMLElement | null>(null);
  const mount = useCallback(
    (element: HTMLElement | null) => {
      if (element) {
        element.append(host);
        setMountedHost(host);
        return;
      }
      setMountedHost(null);
    },
    [host],
  );
  const value = useMemo(
    () => ({ host: mountedHost, mount }),
    [mount, mountedHost],
  );

  return (
    <HeaderActionSlotContext value={value}>{children}</HeaderActionSlotContext>
  );
};

/**
 * The corner itself. The header renders this where the trigger should land;
 * attaching the portal target is a layout effect rather than a callback ref so
 * the host element is never read out of context during render.
 */
export const HeaderActionSlotHost = () => {
  const slot = useContext(HeaderActionSlotContext);
  const anchorRef = useRef<HTMLDivElement>(null);
  const mount = slot?.mount;

  useLayoutEffect(() => {
    if (!mount) return;
    mount(anchorRef.current);
    return () => mount(null);
  }, [mount]);

  if (!slot) {
    return null;
  }

  return <div className="app-navbar__route-actions" ref={anchorRef} />;
};
