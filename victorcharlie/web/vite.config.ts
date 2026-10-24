import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` serves the app with hot reload and sends API, upload and
// media traffic to a Victor Charlie server on :8080.
const target = "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 2000,
  },
  server: {
    proxy: {
      "/api": { target, ws: true },
      "/uploads": target,
      "/rtc": { target, ws: true },
    },
  },
});
