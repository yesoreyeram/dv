import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { App } from "./App";
import "./index.css";
import { createQueryClient } from "./lib/queryClient";
import { applyTheme, getInitialTheme } from "./lib/theme";

// Apply the theme before first paint; inline scripts are forbidden by CSP.
applyTheme(getInitialTheme());

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

createRoot(root).render(
  <StrictMode>
    <BrowserRouter>
      <App queryClient={createQueryClient()} />
    </BrowserRouter>
  </StrictMode>,
);
