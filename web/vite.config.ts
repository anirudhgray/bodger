import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// bodger's REST API always binds loopback (docs/architecture.md §9,
// ADR-0006 — no auth yet). `bodger serve`'s default is 127.0.0.1:8080;
// override with BODGER_DEV_API_PROXY_TARGET if you run it on another port.
const apiProxyTarget =
  process.env.BODGER_DEV_API_PROXY_TARGET ?? 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  server: {
    // Dev-only proxy so `npm run dev` can hit a locally running
    // `bodger serve` without a production build each time (issue #58).
    // The built app never proxies: in production it's the same binary
    // serving both the API and these static assets (ADR-0001).
    proxy: {
      '/api': { target: apiProxyTarget, changeOrigin: true },
      '/healthz': { target: apiProxyTarget, changeOrigin: true },
    },
  },
  build: {
    // `make build-web` (issue #58) needs its output where
    // internal/platform/webui's go:embed directive can find it — see that
    // package's doc comment. Vite refuses to write outside its project
    // root (`web/`) without this explicit opt-in, since it empties the
    // directory first.
    outDir: '../internal/platform/webui/dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
})
