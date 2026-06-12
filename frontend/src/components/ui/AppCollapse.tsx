import React, { useEffect, useState } from "react";

import "./app-collapse.css";

interface AppCollapseProps {
  children: React.ReactNode;
  in: boolean;
  timeout?: number | "auto";
  unmountOnExit?: boolean;
}

const AppCollapse: React.FC<AppCollapseProps> = ({
  in: isOpen,
  timeout = 600,
  unmountOnExit = false,
  children,
}) => {
  const [mounted, setMounted] = useState(isOpen);
  const duration = timeout === "auto" ? 600 : timeout;

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
    }, duration);

    return () => window.clearTimeout(timerId);
  }, [duration, isOpen, mounted, unmountOnExit]);

  if (!isOpen && !mounted && unmountOnExit) return null;

  // Remounted elements must render closed until the collapsed state has
  // painted; permanently-mounted ones can open immediately.
  const open = isOpen && (mounted || !unmountOnExit);

  return (
    <div
      className={`app-collapse ${open ? "app-collapse--open" : ""}`}
      style={{ transitionDuration: `${duration}ms` }}
    >
      <div className="app-collapse__inner">{children}</div>
    </div>
  );
};

export default AppCollapse;
