import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio, CACHE_TTL_MS } from "@/api";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import DevToolsButton from "@/components/footer/DevToolsButton";

function Footer() {
  const theme = useTheme();

  const { data } = linuxio.control.version.useQuery({
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
    retry: false, // Don't retry on failure for footer
  });

  return (
    <div
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
              <Typography
                variant="caption"
                sx={{
                  opacity: 0.6,
                  fontSize: "0.7rem",
                }}
              >
                {data.current_version}
              </Typography>
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
    </div>
  );
}

export default React.memo(Footer);
