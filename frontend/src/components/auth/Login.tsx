import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Alert,
  Box,
  Button,
  IconButton,
  InputAdornment,
  TextField,
} from "@mui/material";
import { keyframes } from "@mui/system";
import React, { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import useAuth from "@/hooks/useAuth";

const reveal = keyframes`
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
`;

const revealSx = (delayMs: number) => ({
  opacity: 0,
  transform: "translateY(10px)",
  animation: `${reveal} 0.6s ease forwards`,
  animationDelay: `${delayMs}ms`,
  "@media (prefers-reduced-motion: reduce)": {
    animation: "none",
    opacity: 1,
    transform: "none",
  },
});

const fieldSx = (theme: any) => ({
  my: 1,

  "& .MuiInputLabel-root": {
    color: theme.palette.text.secondary,
    fontWeight: 500,
  },

  "& .MuiOutlinedInput-root": {
    borderRadius: 3,
    backgroundColor: "rgba(255,255,255,0.9)",
    transition: "box-shadow 0.2s ease, border-color 0.2s ease",
    "& fieldset": { borderColor: "rgba(15,23,42,0.12)" },
    "&:hover fieldset": { borderColor: "rgba(14,165,164,0.4)" },
    "&.Mui-focused fieldset": { borderColor: theme.palette.primary.main },
    "&.Mui-focused": {
      boxShadow: "0 0 0 3px rgba(14,165,164,0.18)",
    },
  },

  // Smaller size on smaller screens
  "& .MuiOutlinedInput-input": {
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

  const handleSubmit = async (e: React.FormEvent) => {
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
        <Alert
          severity="warning"
          sx={{
            mb: 2,
            borderRadius: 2,
            border: "1px solid rgba(249,115,22,0.25)",
            backgroundColor: "rgba(249,115,22,0.12)",
            color: "text.primary",
            ...revealSx(60),
          }}
        >
          {error}
        </Alert>
      )}
      <Box sx={revealSx(140)}>
        <TextField
          label="Username"
          fullWidth
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
          sx={fieldSx}
          slotProps={{ inputLabel: { shrink: true } }}
        />
      </Box>

      <Box sx={revealSx(220)}>
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
                  <IconButton
                    onClick={() => setShowPassword((p) => !p)}
                    edge="end"
                    sx={{
                      color: "text.secondary",
                      "&:hover": { color: "text.primary" },
                    }}
                  >
                    {showPassword ? <VisibilityOffIcon /> : <VisibilityIcon />}
                  </IconButton>
                </InputAdornment>
              ),
            },
          }}
        />
      </Box>

      <Box sx={revealSx(300)}>
        <Button
          type="submit"
          variant="contained"
          fullWidth
          color="primary"
          disabled={loading}
          sx={(theme) => ({
            my: 2,
            py: 1.6,
            borderRadius: 999,
            fontWeight: 600,
            letterSpacing: "0.02em",
            backgroundImage:
              "linear-gradient(135deg, var(--accent), #22c55e)",
            boxShadow: "0 18px 40px -26px rgba(14,165,164,0.8)",
            "&:hover": {
              backgroundImage:
                "linear-gradient(135deg, var(--accent-strong), #16a34a)",
              boxShadow: "0 22px 46px -28px rgba(14,165,164,0.9)",
            },
            "&:active": { transform: "translateY(1px)" },
            [theme.breakpoints.down("md")]: {
              py: 1.25,
            },
            "@media (prefers-reduced-motion: reduce)": {
              transition: "none",
            },
          })}
        >
          Sign in
        </Button>
      </Box>
    </form>
  );
}

export default LogIn;
