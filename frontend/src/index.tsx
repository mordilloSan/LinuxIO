import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import "./theme/variables.css";
import "./icons/icons";
import App from "./App";

// Start the App
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
);
