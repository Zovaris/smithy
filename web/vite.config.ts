import { defineConfig } from "vitest/config";
import preact from "@preact/preset-vite";

export default defineConfig({
  plugins: [preact()],
  resolve: {
    alias: {
      react: "preact/compat",
      "react-dom": "preact/compat",
      "react/jsx-runtime": "preact/jsx-runtime",
    },
  },
  build: {
    outDir: "../internal/web/dist/app",
    emptyOutDir: true,
    target: "es2022",
    cssCodeSplit: true,
    modulePreload: { polyfill: false },
  },
  test: { environment: "node" },
});
