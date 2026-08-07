import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@fontsource-variable/inter/wght.css";

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

if (import.meta.env.DEV) {
  // Buffered PerformanceObserver entries let development diagnostics load
  // after the critical startup work without losing initial page metrics.
  window.addEventListener(
    "load",
    () => {
      void import("./performance/startWebVitals")
        .then(({ startWebVitals }) => startWebVitals())
        .catch((error: unknown) => {
          console.warn("Unable to start Web Vitals measurement", error);
        });
    },
    { once: true },
  );
}
