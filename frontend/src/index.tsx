import * as EmotionReact from "@emotion/react";
import * as EmotionStyled from "@emotion/styled";
import * as MaterialUI from "@mui/material";
import * as React from "react";
import * as ReactDOM from "react-dom";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import App from "./App";

// Expose dependencies as window globals for dynamic modules
(window as any).React = React;
(window as any).ReactDOM = ReactDOM;
(window as any).MaterialUI = MaterialUI;
(window as any).EmotionReact = EmotionReact;
(window as any).EmotionStyled = EmotionStyled;

// Start the App
const container = document.getElementById("root");
const root = createRoot(container!);

root.render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
);
