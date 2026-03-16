import { Button } from "@mui/material";
import React from "react";
import { Link } from "react-router-dom";

import AppTypography from "@/components/ui/AppTypography";

function Page404() {
  return (
    <div style={{ textAlign: "center" }}>
      <AppTypography component="h1" variant="h1" align="center" gutterBottom>
        404
      </AppTypography>
      <AppTypography component="h2" variant="h4" align="center" gutterBottom>
        Page not found.
      </AppTypography>
      <AppTypography
        component="h2"
        variant="subtitle1"
        align="center"
        gutterBottom
      >
        The page you are looking for might have been removed.
      </AppTypography>

      <Button
        component={Link}
        to="/"
        variant="contained"
        color="secondary"
        sx={{ mt: 2 }}
      >
        Return to website
      </Button>
    </div>
  );
}

export default Page404;
