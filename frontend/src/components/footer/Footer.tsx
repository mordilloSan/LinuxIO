import { Grid, Box, useTheme } from "@mui/material";
import React from "react";

function Footer() {
  const theme = useTheme();

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
        ></Grid>
      </Grid>
    </Box>
  );
}

export default Footer;
