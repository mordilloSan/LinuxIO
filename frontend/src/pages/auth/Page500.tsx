import { Button } from "@mui/material";
import React from "react";
import { Link } from "react-router-dom";

import AppTypography from "@/components/ui/AppTypography";

function Page500() {
  return (
    <div style={{ textAlign: "center" }}>
      <AppTypography component="h1" variant="h1" align="center" gutterBottom>
        500
      </AppTypography>
      <AppTypography component="h2" variant="h4" align="center" gutterBottom>
        Internal server error.
      </AppTypography>
      <AppTypography
        component="h2"
        variant="subtitle1"
        align="center"
        gutterBottom
      >
        The server encountered something unexpected that didn’t allow it to
        complete the request.
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

export default Page500;
