import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Em desenvolvimento, o Vite (porta 5173) encaminha /api e /auth para o agente.
// O agente só aceita a própria origem, então o proxy reescreve Host e Origin.
const agent = "http://127.0.0.1:47821";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../agent/internal/webui/static",
    emptyOutDir: false, // o Makefile limpa; static/README fica versionado
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
