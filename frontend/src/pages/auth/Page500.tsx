import { Link } from "react-router-dom";

import AppButton from "@/components/ui/AppButton";
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

      <Link
        to="/"
        style={{
          textDecoration: "none",
          marginTop: 16,
          display: "inline-block",
        }}
      >
        <AppButton variant="contained" color="secondary">
          Return to website
        </AppButton>
      </Link>
    </div>
  );
}

export default Page500;
