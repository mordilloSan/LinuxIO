import { Box, useTheme, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import ErrorBoundary from "@/components/errors/ErrorBoundary";
import DownloadNotifications from "@/components/filebrowser/DownloadNotifications";
import axios from "@/utils/axios";

interface VersionResponse {
  checked_at: string;
  current_version: string;
  latest_version: string;
  update_available: boolean;
}

function Footer() {
  const theme = useTheme();

  const { data } = useQuery<VersionResponse>({
    queryKey: ["version"],
    queryFn: async () => {
      const res = await axios.get("/control/version");
      return res.data;
    },
    staleTime: 5 * 60 * 1000, // Cache for 5 minutes
    retry: false, // Don't retry on failure for footer
  });

  return (
    <Box
      sx={{
        width: "100%",
        background: theme.footer?.background || theme.palette.background.paper,
        position: "relative",
        zIndex: 1300,
      }}
    >
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          px: 1,
        }}
      >
        <Box
          sx={{
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
        </Box>
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            ml: "auto",
          }}
        >
          <ErrorBoundary>
            <DownloadNotifications />
          </ErrorBoundary>
        </Box>
      </Box>
    </Box>
  );
}

export default Footer;
