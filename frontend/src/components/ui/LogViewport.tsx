import type { RefObject } from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert from "@/components/ui/AppAlert";
import AppTypography from "@/components/ui/AppTypography";

import "./log-viewport.css";

interface LogViewportProps {
  error: string | null;
  isLoading: boolean;
  logs: string;
  /** Scroll container; `useLogStream` pins it to the newest line while live. */
  logsBoxRef: RefObject<HTMLDivElement | null>;
  /** card: rounded and fills its card. dialog: flush, taller, error as an alert. */
  variant?: "card" | "dialog";
}

/** The live log surface shared by log cards and log dialogs. */
const LogViewport = ({
  error,
  isLoading,
  logs,
  logsBoxRef,
  variant = "card",
}: LogViewportProps) => {
  if (variant === "dialog" && error) {
    return (
      <AppAlert severity="error" style={{ margin: 8 }}>
        {error}
      </AppAlert>
    );
  }

  return (
    <div className={`log-viewport log-viewport--${variant}`}>
      {isLoading && (
        <div className="log-viewport__loading">
          <ComponentLoader />
        </div>
      )}
      {error && <div className="log-viewport__error">{error}</div>}
      <AppTypography
        className="log-viewport__box"
        component="div"
        ref={logsBoxRef}
        variant="body2"
      >
        {!isLoading &&
          !error &&
          (logs || (
            <AppTypography
              color="text.secondary"
              component="span"
              variant="caption"
            >
              No logs available.
            </AppTypography>
          ))}
      </AppTypography>
    </div>
  );
};

export default LogViewport;
