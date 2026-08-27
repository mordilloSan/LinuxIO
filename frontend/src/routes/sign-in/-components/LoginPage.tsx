import AppTypography from "@/components/ui/AppTypography";
import LoginComponent from "@/routes/sign-in/-components/Login";

import "./login-page.css";

const LoginPage = () => {
  return (
    <div style={{ width: "100%", maxWidth: 520, position: "relative" }}>
      <div
        className="login-badge"
        style={{
          borderRadius: "9999px",
          border:
            "1px solid color-mix(in srgb, var(--app-palette-text-secondary), transparent 75%)",
          background:
            "linear-gradient(160deg, color-mix(in srgb, var(--app-palette-background-paper), transparent 5%) 0%, color-mix(in srgb, var(--app-palette-background-default), transparent 8%) 100%)",
          boxShadow:
            "0 24px 54px -36px color-mix(in srgb, black, transparent 15%)",
          display: "inline-flex",
          alignItems: "center",
          gap: 4,
          backdropFilter: "blur(10px)",
          WebkitBackdropFilter: "blur(10px)",
        }}
      >
        <AppTypography
          color="var(--app-palette-text-primary)"
          component="span"
          fontWeight={600}
          style={{ letterSpacing: "0.06em" }}
          variant="body2"
        >
          Linux
        </AppTypography>
        <div
          className="login-badge-icon"
          style={{
            borderRadius: "50%",
            border: "2px solid var(--app-palette-primary-main)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <AppTypography
            color="var(--app-palette-primary-main)"
            component="span"
            fontWeight={700}
            style={{ letterSpacing: "-0.02em" }}
            variant="body2"
          >
            i/O
          </AppTypography>
        </div>
      </div>

      <div
        className="login-paper panel"
        style={{
          borderRadius: "calc(var(--app-radius-base) * 4)",
          backgroundColor:
            "color-mix(in srgb, var(--app-palette-background-default), transparent 10%)",
          backgroundImage:
            "linear-gradient(color-mix(in srgb, white, transparent 94.9%), color-mix(in srgb, white, transparent 94.9%))",
          border:
            "1px solid color-mix(in srgb, var(--app-palette-text-secondary), transparent 80%)",
          boxShadow:
            "0 26px 60px -40px color-mix(in srgb, black, transparent 25%)",
          backdropFilter: "blur(14px)",
          WebkitBackdropFilter: "blur(14px)",
        }}
      >
        <div
          style={{
            display: "grid",
            gap: "var(--app-space-4)",
            textAlign: "center",
            marginBottom: "var(--app-space-8)",
          }}
        >
          <AppTypography
            component="h1"
            fontWeight={600}
            style={{ letterSpacing: "-0.02em" }}
            variant="h1"
          >
            Welcome back
          </AppTypography>
          <AppTypography className="text-muted" component="p" variant="body2">
            Sign in to manage your Linux i/O instance.
          </AppTypography>
        </div>
        <LoginComponent />
      </div>
    </div>
  );
};

export default LoginPage;
