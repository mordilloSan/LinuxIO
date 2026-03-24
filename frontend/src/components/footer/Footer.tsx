import React from "react";

import { linuxio, CACHE_TTL_MS } from "@/api";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import DevToolsButton from "@/components/footer/DevToolsButton";
import { useAppTheme } from "@/theme";

function Footer() {
  const theme = useAppTheme();

  const { data } = linuxio.control.version.useQuery({
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
    retry: false, // Don't retry on failure for footer
  });

  return (
    <footer
      style={{
        width: "100%",
        background: theme.footer?.background || theme.palette.background.paper,
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
        </div>
      </div>
    </footer>
  );
}

export default React.memo(Footer);
