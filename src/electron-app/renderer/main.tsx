import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

/**
 * In the dev server, `?mock=1` swaps the Electron preload bridge for a fixture
 * so the interface can be driven and screenshotted in a plain browser.
 * The dynamic import is tree-shaken out of the release bundle.
 */
async function bootstrap() {
  const dev = (import.meta as any).env?.DEV;
  if (dev && new URLSearchParams(window.location.search).has("mock")) {
    const { installMock } = await import("./mock");
    installMock();
  }

  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>
  );
}

bootstrap();
