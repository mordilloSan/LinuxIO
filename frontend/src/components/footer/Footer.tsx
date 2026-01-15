import { Box, useTheme, Typography } from "@mui/material";

import linuxio from "@/api/react-query";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import FileNotifications from "@/components/filebrowser/FileNotifications";
import DevToolsButton from "@/components/footer/DevToolsButton";

function Footer() {
  const theme = useTheme();

  const { data } = linuxio.control.version.useQuery({
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
            gap: 1,
            ml: "auto",
          }}
        >
          <ErrorBoundary>
            <DevToolsButton />
          </ErrorBoundary>
          <ErrorBoundary>
            <FileNotifications />
          </ErrorBoundary>
        </Box>
      </Box>
    </Box>
  );
}

export default Footer;
