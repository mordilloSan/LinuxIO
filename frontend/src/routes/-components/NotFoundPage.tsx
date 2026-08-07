import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";

function NotFoundPage() {
  return (
    <div style={{ textAlign: "center" }}>
      <h1 style={{ margin: 0, fontSize: "2rem", lineHeight: 1.25 }}>404</h1>
      <h2
        className="section-title"
        style={{ marginTop: 8, marginBottom: 8, textAlign: "center" }}
      >
        Page not found.
      </h2>
      <p
        className="text-muted"
        style={{ margin: 0, fontSize: "0.9286rem", lineHeight: 1.75 }}
      >
        The page you are looking for might have been removed.
      </p>

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
