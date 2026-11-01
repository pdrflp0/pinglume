import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { SimulationBanner } from "./Simulation";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <SimulationBanner />
    <App />
  </StrictMode>,
);
