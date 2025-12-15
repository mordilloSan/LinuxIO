import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  TextField,
  Button,
  Alert,
  InputAdornment,
  IconButton,
} from "@mui/material";
import React, { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import useAuth from "@/hooks/useAuth";

const BORDER_COLOR = "rgba(255,255,255,0.25)";

const fieldSx = (theme: any) => ({
  my: 1,

  // label stays grey, even when focused
  "& .MuiInputLabel-root": { color: BORDER_COLOR },

  // outline: static on idle/hover, primary on focus
  "& .MuiOutlinedInput-root": {
    "&:hover .MuiOutlinedInput-notchedOutline": { borderColor: BORDER_COLOR },
    "&.Mui-focused .MuiOutlinedInput-notchedOutline": {
      borderColor: theme.palette.primary.main,
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
        <Alert severity="warning" sx={{ mb: 3 }}>
          {error}
        </Alert>
      )}
      <TextField
        label="Username"
        fullWidth
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        autoComplete="username"
        sx={fieldSx}
        slotProps={{ inputLabel: { shrink: true } }}
      />

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
                >
                  {showPassword ? <VisibilityOffIcon /> : <VisibilityIcon />}
                </IconButton>
              </InputAdornment>
            ),
          },
        }}
      />

      <Button
        type="submit"
        variant="contained"
        fullWidth
        color="primary"
        disabled={loading}
        sx={{
          my: 2,
          py: 2,
        }}
      >
        Log in
      </Button>
    </form>
  );
}

export default LogIn;
