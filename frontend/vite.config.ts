import path from "node:path";
import { defineConfig, type Plugin } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import wails from "@wailsio/runtime/plugins/vite";

/**
 * CSP del paquete (solo en `build`: el servidor de desarrollo de Vite inyecta scripts en línea). Sin scripts ni
 * objetos externos; el servidor remoto es arbitrario, así que `connect-src`/`img-src`/`media-src` admiten http(s)/ws(s).
 */
export const CSP = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob: http: https:",
  "media-src 'self' blob: data: http: https:",
  "font-src 'self' data:",
  "connect-src 'self' http: https: ws: wss:",
  "object-src 'none'",
  "frame-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
].join("; ");

const cspPlugin = (): Plugin => ({
  name: "dc-csp",
  apply: "build",
  transformIndexHtml: () => [
    { tag: "meta", attrs: { "http-equiv": "Content-Security-Policy", content: CSP }, injectTo: "head-prepend" },
    { tag: "meta", attrs: { name: "referrer", content: "no-referrer" }, injectTo: "head-prepend" },
  ],
});

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "src") },
  },
  plugins: [react(), tailwindcss(), wails("./bindings"), cspPlugin()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
