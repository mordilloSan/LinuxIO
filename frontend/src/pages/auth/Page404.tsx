import { Button, Typography } from "@mui/material";
import React from "react";
import { Link } from "react-router-dom";

function Page404() {
  return (
    <div style={{ textAlign: "center" }}>
      <Typography component="h1" variant="h1" align="center" gutterBottom>
        404
      </Typography>
      <Typography component="h2" variant="h4" align="center" gutterBottom>
        Page not found.
      </Typography>
      <Typography
        component="h2"
        variant="subtitle1"
        align="center"
        gutterBottom
      >
        The page you are looking for might have been removed.
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
    </div>
  );
}

export default Page404;
