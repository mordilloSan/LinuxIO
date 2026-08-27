import { useQuery } from "@tanstack/react-query";
import { memo } from "react";

import { CACHE_TTL_MS, linuxio } from "@/api";
import ErrorBoundary from "@/components/errors/ErrorBoundary";

import DevToolsButton from "./DevToolsButton";
import { NavbarNotificationsDropdown } from "../navbar/NavbarNotificationsDropdown";

function Footer() {
  const { data } = useQuery({
    ...linuxio.control.version,
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
    retry: false, // Don't retry on failure for footer
  });

  return (
    <footer
      style={{
        width: "100%",
        background: "var(--app-footer-background)",
        position: "relative",
        zIndex: 1300,
      }}
    >
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          paddingInline: 4,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-start",
            flexShrink: 0,
          }}
        >
          <ErrorBoundary>
            {data?.current_version && (
              <small
                className="text-caption"
                style={{
                  opacity: 0.6,
                  letterSpacing: "normal",
                  transform: "translateY(5px)",
                }}
              >
                {data.current_version}
              </small>
            )}
          </ErrorBoundary>
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 4,
            marginLeft: "auto",
          }}
        >
          <ErrorBoundary>
            <DevToolsButton />
          </ErrorBoundary>
          <ErrorBoundary>
            <NavbarNotificationsDropdown />
          </ErrorBoundary>
        </div>
      </div>
    </footer>
  );
}

export default memo(Footer);
