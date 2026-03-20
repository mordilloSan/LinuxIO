import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import "./styles/variables.css";
import "./lib/icons"; // pre-registers all app icons at startup — no CDN calls
import App from "./App";

// Start the App
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
);
