import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import "./theme/variables.css";
import "./icons/icons";
import App from "./App";
import { MOTION_CSS_VARS } from "./theme/constants";

// Motion design tokens are theme-invariant. Apply them to :root before the
// first paint so loaders that render before AppThemeProvider mounts (e.g. the
// PageLoader's linear progress) already have the easing/duration variables.
for (const [key, value] of Object.entries(MOTION_CSS_VARS)) {
  document.documentElement.style.setProperty(key, value);
}

// Start the App
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <BrowserRouter useTransitions>
    <App />
  </BrowserRouter>,
);
