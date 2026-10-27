import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const agent = "http://127.0.0.1:47821";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../agent/internal/webui/static",
    emptyOutDir: false,
    sourcemap: false,
  },
  server: {
    proxy: {
      "/api": {
        target: agent,
        changeOrigin: true,
        headers: { Origin: agent },
      },
      "/auth": { target: agent, changeOrigin: true },
    },
  },
  test: { environment: "jsdom", globals: true },
});
