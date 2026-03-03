import { createTheme } from "@mui/material/styles";

const authTheme = createTheme({
  palette: {
    mode: "dark",
    primary: {
      main: "#407AD6",
      light: "#6395E0",
      dark: "#2F65CB",
      contrastText: "#FFF",
    },
    secondary: {
      main: "#6395E0",
      contrastText: "#FFF",
    },
    background: {
      default: "#1B2635",
      paper: "#233044",
    },
    text: {
      primary: "rgba(255, 255, 255, 0.95)",
      secondary: "rgba(255, 255, 255, 0.6)",
    },
  },
  shape: { borderRadius: 16 },
  typography: {
    fontFamily: '"Space Grotesk", "Sora", sans-serif',
    button: { textTransform: "none", fontWeight: 600 },
    h4: { fontWeight: 600, letterSpacing: "-0.02em" },
  },
});

export default authTheme;
