import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "./theme/variables.css";
import "./icons/shell";
import App from "./App";
import { applyCssVariables } from "./theme";
import { MOTION_CSS_VARS } from "./theme/constants";

applyCssVariables(document.documentElement, MOTION_CSS_VARS);
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <StrictMode>
    <App />
  </StrictMode>,
);
