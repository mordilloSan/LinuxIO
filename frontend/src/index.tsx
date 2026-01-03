import * as MaterialUI from "@mui/material";
import * as React from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import App from "./App";

// Expose React and Material-UI as window globals for dynamic modules
(window as any).React = React;
(window as any).MaterialUI = MaterialUI;

// Start the App
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
);
