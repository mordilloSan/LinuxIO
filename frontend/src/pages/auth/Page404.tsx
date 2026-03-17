import { Link } from "react-router-dom";

import AppButton from "@/components/ui/AppButton";
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

export default Page404;
