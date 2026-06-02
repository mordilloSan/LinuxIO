import { Icon } from "@iconify/react";
import React, { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTextField from "@/components/ui/AppTextField";
import "./login.css";
import useAuth from "@/hooks/useAuth";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

function LogIn() {
  const theme = useAppTheme();
  const fieldStyle = {
    "--lf-bg": alpha(theme.palette.background.default, 0.65),
    "--lf-border": alpha(theme.palette.text.secondary, 0.3),
    "--lf-border-hover": alpha(theme.palette.text.secondary, 0.55),
    "--lf-focus-color": theme.palette.primary.main,
    "--lf-focus-shadow": alpha(theme.palette.primary.main, 0.28),
    "--lf-label-color": theme.palette.text.secondary,
  } as React.CSSProperties;

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const redirect = searchParams.get("redirect") || "/";
  const { signIn } = useAuth();

  const handleSubmit = async (e: React.SyntheticEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError(null);

    if (!username || !password) {
      setError("Username and password are required.");
      return;
    }

    try {
      setLoading(true);
      await signIn(username, password);
      navigate(redirect);
    } catch (err: any) {
      setError(err.message || "Something went wrong");
    } finally {
      setLoading(false);
    }
  };

  return (
    <form noValidate onSubmit={handleSubmit}>
      {error && (
        <AppAlert
          className="login-alert login-reveal"
          severity="warning"
          style={
            {
              "--login-reveal-delay": "60ms",
              "--login-alert-bg": alpha(theme.palette.warning.main, 0.18),
              "--login-alert-border": alpha(theme.palette.warning.main, 0.36),
              "--login-alert-icon": theme.palette.warning.main,
              "--login-alert-text": alpha(theme.palette.common.white, 0.92),
              marginBottom: 16,
            } as React.CSSProperties
          }
        >
          {error}
        </AppAlert>
      )}
      <div
        className="login-reveal"
        style={{ "--login-reveal-delay": "140ms" } as React.CSSProperties}
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
        style={{ "--login-reveal-delay": "220ms" } as React.CSSProperties}
      >
        <AppTextField
          autoComplete="current-password"
          className="login-field"
          endAdornment={
            <AppIconButton
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
        style={{ "--login-reveal-delay": "300ms" } as React.CSSProperties}
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
