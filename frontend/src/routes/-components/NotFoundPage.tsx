import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppTypography from "@/components/ui/AppTypography";

function NotFoundPage() {
  return (
    <div style={{ textAlign: "center" }}>
      <AppTypography component="h1" variant="h1">
        404
      </AppTypography>
      <h2
        className="section-title"
        style={{ marginTop: 8, marginBottom: 8, textAlign: "center" }}
      >
        Page not found.
      </h2>
      <AppTypography className="text-muted" component="p" variant="subtitle1">
        The page you are looking for might have been removed.
      </AppTypography>

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
    </div>
  );
}

export default NotFoundPage;
