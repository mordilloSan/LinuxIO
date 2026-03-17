import React, { useEffect, useState } from "react";

import "./app-collapse.css";

interface AppCollapseProps {
  in: boolean;
  timeout?: number | "auto";
  unmountOnExit?: boolean;
  children: React.ReactNode;
}

const AppCollapse: React.FC<AppCollapseProps> = ({
  in: isOpen,
  timeout = 300,
  unmountOnExit = false,
  children,
}) => {
  const [mounted, setMounted] = useState(isOpen);
  const duration = timeout === "auto" ? 300 : timeout;

  useEffect(() => {
    if (isOpen) {
      const frameId = window.requestAnimationFrame(() => {
        setMounted(true);
      });
      return () => window.cancelAnimationFrame(frameId);
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

  return (
    <div
      className={`app-collapse ${isOpen ? "app-collapse--open" : ""}`}
      style={{ transitionDuration: `${duration}ms` }}
    >
      <div className="app-collapse__inner">{children}</div>
    </div>
  );
};

export default AppCollapse;
