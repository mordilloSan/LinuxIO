import { Icon } from "@iconify/react";
import { InputAdornment, TextField } from "@mui/material";
import React, { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import "./login.css";
import useAuth from "@/hooks/useAuth";
import { alpha } from "@/utils/color";

const fieldSx = (theme: any) => ({
  my: 1,

  "& .MuiInputLabel-root": {
    color: theme.palette.text.secondary,
    fontWeight: 500,
  },

  "& .MuiOutlinedInput-root": {
    borderRadius: 3,
    backgroundColor: alpha(theme.palette.background.default, 0.65),
    transition: "box-shadow 0.2s ease, border-color 0.2s ease",
    "& fieldset": { borderColor: alpha(theme.palette.text.secondary, 0.3) },
    "&:hover fieldset": {
      borderColor: alpha(theme.palette.text.secondary, 0.55),
    },
    "&.Mui-focused fieldset": { borderColor: theme.palette.primary.main },
    "&.Mui-focused": {
      boxShadow: `0 0 0 3px ${alpha(theme.palette.primary.main, 0.35)}`,
    },
  },

  // Smaller size on smaller screens
  "& .MuiOutlinedInput-input": {
    color: theme.palette.text.primary,
    [theme.breakpoints.down("md")]: {
      padding: "8.5px 14px",
    },
  },
});

function LogIn() {
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
          severity="warning"
          className="login-reveal"
          style={
            {
              "--login-reveal-delay": "60ms",
              marginBottom: 16,
              borderRadius: 16,
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
        <TextField
          label="Username"
          fullWidth
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
          sx={fieldSx}
          slotProps={{ inputLabel: { shrink: true } }}
        />
      </div>

      <div
        className="login-reveal"
        style={{ "--login-reveal-delay": "220ms" } as React.CSSProperties}
      >
        <TextField
          label="Password"
          type={showPassword ? "text" : "password"}
          fullWidth
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          sx={fieldSx}
          slotProps={{
            inputLabel: { shrink: true },
            input: {
              endAdornment: (
                <InputAdornment position="end">
                  <AppIconButton
                    onClick={() => setShowPassword((p) => !p)}
                    edge="end"
                    className="login-password-toggle"
                  >
                    {showPassword ? (
                      <Icon icon="mdi:eye-off" width={22} height={22} />
                    ) : (
                      <Icon icon="mdi:eye" width={22} height={22} />
                    )}
                  </AppIconButton>
                </InputAdornment>
              ),
            },
          }}
        />
      </div>

      <div
        className="login-reveal"
        style={{ "--login-reveal-delay": "300ms" } as React.CSSProperties}
      >
        <AppButton
          type="submit"
          variant="contained"
          fullWidth
          color="primary"
          disabled={loading}
          className="login-submit-btn"
        >
          Sign in
        </AppButton>
      </div>
    </form>
  );
}

export default LogIn;
