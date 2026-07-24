import { Link } from "@tanstack/react-router";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

interface ErrorPageProps {
  error?: Error;
  onRetry?: () => void;
}

function ErrorPage({ error, onRetry }: ErrorPageProps) {
  return (
    <div style={{ textAlign: "center" }}>
      <AppTypography align="center" component="h1" gutterBottom variant="h1">
        500
      </AppTypography>
      <AppTypography align="center" component="h2" gutterBottom variant="h4">
        Internal server error.
      </AppTypography>
      <AppTypography
        align="center"
        component="h2"
        gutterBottom
        variant="subtitle1"
      >
        {error?.message ??
          "The server encountered something unexpected that didn’t allow it to complete the request."}
      </AppTypography>

      {onRetry ? (
        <AppButton
          color="secondary"
          onClick={onRetry}
          style={{ marginTop: 16 }}
          variant="contained"
        >
          Try again
        </AppButton>
      ) : (
        <Link
          style={{
            textDecoration: "none",
            marginTop: 16,
            display: "inline-block",
          }}
          to="/"
        >
          <AppButton color="secondary" variant="contained">
            Return to website
          </AppButton>
        </Link>
      )}
    </div>
  );
}

export default ErrorPage;
