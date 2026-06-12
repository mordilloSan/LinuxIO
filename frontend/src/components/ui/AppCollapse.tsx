import React, { useEffect, useState } from "react";

import "./app-collapse.css";

// Single source of truth for every expand/collapse animation in the app:
// the collapse itself plus anything that moves with it (chevrons, fades).
export const COLLAPSE_DURATION_MS = 600;
export const COLLAPSE_TRANSITION = `${COLLAPSE_DURATION_MS}ms cubic-bezier(0.4, 0, 0.2, 1)`;

interface AppCollapseProps {
  children: React.ReactNode;
  in: boolean;
  unmountOnExit?: boolean;
}

const AppCollapse: React.FC<AppCollapseProps> = ({
  in: isOpen,
  unmountOnExit = false,
  children,
}) => {
  const [mounted, setMounted] = useState(isOpen);

  useEffect(() => {
    if (isOpen) {
      // Two frames: the browser must paint the collapsed state first,
      // or a freshly remounted element pops open without transitioning.
      let secondFrameId: number | undefined;
      const frameId = window.requestAnimationFrame(() => {
        secondFrameId = window.requestAnimationFrame(() => {
          setMounted(true);
        });
      });
      return () => {
        window.cancelAnimationFrame(frameId);
        if (secondFrameId !== undefined) {
          window.cancelAnimationFrame(secondFrameId);
        }
      };
    }

    if (!mounted || !unmountOnExit) {
      return undefined;
    }

    const timerId = window.setTimeout(() => {
      setMounted(false);
    }, COLLAPSE_DURATION_MS);

    return () => window.clearTimeout(timerId);
  }, [isOpen, mounted, unmountOnExit]);

  if (!isOpen && !mounted && unmountOnExit) return null;

  // Remounted elements must render closed until the collapsed state has
  // painted; permanently-mounted ones can open immediately.
  const open = isOpen && (mounted || !unmountOnExit);

  return (
    <div
      className={`app-collapse ${open ? "app-collapse--open" : ""}`}
      style={{ transitionDuration: `${COLLAPSE_DURATION_MS}ms` }}
    >
      <div className="app-collapse__inner">{children}</div>
    </div>
  );
};

export default AppCollapse;
