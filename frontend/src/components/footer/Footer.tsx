import { Grid, Box, useTheme, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import axios from "@/utils/axios";

interface VersionResponse {
  output: {
    checked_at: string;
    current_version: string;
    latest_version: string;
    update_available: boolean;
  };
  status: string;
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
        background: theme.footer?.background || theme.palette.background.paper,
        position: "relative",
      }}
    >
      <Grid container spacing={0}>
        {/* Left side links */}
        <Grid
          size={{
            xs: 12,
            md: 6,
          }}
          sx={{ display: { xs: "none", md: "block" } }}
        ></Grid>

        {/* Right side copyright */}
        <Grid
          size={{
            xs: 12,
            md: 6,
          }}
          container
          justifyContent="flex-end"
        >
          {data?.output.current_version && (
            <Typography
              variant="caption"
              sx={{
                opacity: 0.6,
                fontSize: "0.7rem",
                padding: 1,
              }}
            >
              {data.output.current_version}
            </Typography>
          )}
        </Grid>
      </Grid>
    </Box>
  );
}

export default Footer;
