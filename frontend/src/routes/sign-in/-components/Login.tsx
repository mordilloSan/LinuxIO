import { Icon } from "@iconify/react";
import {
  useEffect,
  useState,
  type CSSProperties,
  type SyntheticEvent,
} from "react";

import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
import "./login.css";
import useAuth from "@/hooks/useAuth";
import {
  clearSigninNotice,
  readSigninNotice,
  SIGNIN_NOTICE_MESSAGES,
  type SigninNotice,
} from "@/utils/signinNotice";

const fieldStyle = {
  "--lf-bg":
    "color-mix(in srgb, var(--app-palette-background-default), transparent 35%)",
  "--lf-border":
    "color-mix(in srgb, var(--app-palette-text-secondary), transparent 70%)",
  "--lf-border-hover":
    "color-mix(in srgb, var(--app-palette-text-secondary), transparent 45%)",
  "--lf-focus-color": "var(--app-palette-primary-main)",
  "--lf-focus-shadow":
    "color-mix(in srgb, var(--app-palette-primary-main), transparent 72%)",
  "--lf-label-color": "var(--app-palette-text-secondary)",
} as CSSProperties;

function LogIn() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<SigninNotice | null>(readSigninNotice);
  const [loading, setLoading] = useState(false);

  const { signIn } = useAuth();

  // Clear the external one-shot value after its initial snapshot is committed.
  useEffect(() => {
    clearSigninNotice();
  }, []);

  // A login error takes precedence over the session notice.
  const feedback = error
    ? {
        message: error,
        severity: "warning" as const,
        accent: "var(--app-palette-warning-main)",
      }
    : notice
      ? {
          message: SIGNIN_NOTICE_MESSAGES[notice],
          severity: "info" as const,
          accent: "var(--app-palette-info-main)",
        }
      : null;

  const handleSubmit = async (e: SyntheticEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError(null);
    setNotice(null);

    if (!username || !password) {
      setError("Username and password are required.");
      return;
    }

    const submit = async () => {
      try {
        setLoading(true);
        await signIn(username, password);
      } catch (err: any) {
        setError(err.message || "Something went wrong");
      }
    };
    await submit().finally(() => setLoading(false));
  };

  return (
    <form noValidate onSubmit={handleSubmit}>
      {feedback && (
        <AppAlert
          className="login-alert login-reveal"
          severity={feedback.severity}
          style={
            {
              "--login-reveal-delay": "60ms",
              "--login-alert-bg": `color-mix(in srgb, ${feedback.accent}, transparent 82%)`,
              "--login-alert-border": `color-mix(in srgb, ${feedback.accent}, transparent 64%)`,
              "--login-alert-icon": feedback.accent,
              "--login-alert-text": "color-mix(in srgb, white, transparent 8%)",
              marginBottom: 16,
            } as CSSProperties
          }
        >
          {feedback.message}
        </AppAlert>
      )}
      <div
        className="login-reveal"
        style={{ "--login-reveal-delay": "140ms" } as CSSProperties}
      >
        <AppTextField
          autoComplete="username"
          className="login-field"
          fullWidth
          label="Username"
          onChange={(e) => setUsername(e.target.value)}
          shrinkLabel
          style={fieldStyle}
          value={username}
        />
      </div>

      <div
        className="login-reveal"
        style={{ "--login-reveal-delay": "220ms" } as CSSProperties}
      >
        <AppTextField
          autoComplete="current-password"
          className="login-field"
          endAdornment={
            <AppIconButton
              aria-label={showPassword ? "Hide password" : "Show password"}
              className="login-password-toggle"
              edge="end"
              onClick={() => setShowPassword((p) => !p)}
            >
              {showPassword ? (
                <Icon height={22} icon="mdi:eye-off" width={22} />
              ) : (
                <Icon height={22} icon="mdi:eye" width={22} />
              )}
            </AppIconButton>
          }
          fullWidth
          label="Password"
          onChange={(e) => setPassword(e.target.value)}
          shrinkLabel
          style={fieldStyle}
          type={showPassword ? "text" : "password"}
          value={password}
        />
      </div>

      <div
        className="login-reveal"
        style={{ "--login-reveal-delay": "300ms" } as CSSProperties}
      >
        <AppButton
          className="login-submit-btn"
          color="primary"
          disabled={loading}
          fullWidth
          type="submit"
          variant="contained"
        >
          Sign in
        </AppButton>
      </div>
    </form>
  );
}

export default LogIn;
