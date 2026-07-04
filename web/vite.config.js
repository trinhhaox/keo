import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` proxy API về Go server (:8080) — không đụng CORS.
// Bản build (web/dist) do chính Go server serve nên same-origin sẵn.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/v1": "http://localhost:8080",
      "/callbacks": "http://localhost:8080",
      "/webhooks": "http://localhost:8080",
      "/oauth": "http://localhost:8080",
    },
  },
});
