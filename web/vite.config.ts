/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import fs from "node:fs";

const outDir = path.resolve(import.meta.dirname, "../internal/webui/dist");

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      // Vite empties outDir; the Go embed pattern needs the placeholder to stay.
      name: "keep-gitkeep",
      closeBundle() {
        fs.writeFileSync(path.join(outDir, ".gitkeep"), "");
      },
    },
  ],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  build: { outDir, emptyOutDir: true },
  server: {
    // The Host header stays localhost:5173, which removarr accepts, and the Origin
    // matches it (spec 10). Start removarr on 7272 first.
    proxy: { "/api": { target: "http://127.0.0.1:7272", changeOrigin: false } },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
