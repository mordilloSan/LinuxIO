import { Button, Typography, Box } from "@mui/material";
import React from "react";
import { Link } from "react-router-dom";

function Page500() {
  return (
    <Box sx={{ textAlign: "center" }}>
      <Typography component="h1" variant="h1" align="center" gutterBottom>
        500
      </Typography>
      <Typography component="h2" variant="h4" align="center" gutterBottom>
        Internal server error.
      </Typography>
      <Typography
        component="h2"
        variant="subtitle1"
        align="center"
        gutterBottom
      >
        The server encountered something unexpected that didn’t allow it to
        complete the request.
      </Typography>

      <Button
        component={Link}
        to="/"
        variant="contained"
        color="secondary"
        sx={{ mt: 2 }}
      >
        Return to website
      </Button>
    </Box>
  );
}

export default Page500;
