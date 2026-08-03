import AppButton from "@/components/ui/AppButton";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
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
        <AppRouterLinkButton
          color="secondary"
          style={{
            textDecoration: "none",
            marginTop: 16,
            display: "inline-block",
          }}
          to="/"
          variant="contained"
        >
          Return to website
        </AppRouterLinkButton>
      )}
    </div>
  );
}

export default ErrorPage;
