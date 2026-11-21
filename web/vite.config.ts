import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const target = process.env.HUB_URL || "http://127.0.0.1:7788";

export default defineConfig({
  plugins: [react()],
  build: { outDir: "../server/webdist", emptyOutDir: true, chunkSizeWarningLimit: 1500 },
  server: {
    proxy: {
      "/api": target,
      "/ws": { target: target.replace(/^http/, "ws"), ws: true },
    },
  },
});
