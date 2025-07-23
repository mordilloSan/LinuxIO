import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import App from "./App";

import { ThemeProvider } from "@/contexts/ThemeContext";

// This hides the loading from index.html
const splash = document.getElementById("splash");
if (splash) {
  splash.style.opacity = "0";
  splash.remove();
}

// Start the App
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <BrowserRouter>
    <ThemeProvider>
      <App />
    </ThemeProvider>
  </BrowserRouter>,
);
