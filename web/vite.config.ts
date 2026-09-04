import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import mkcert from 'vite-plugin-mkcert'
import { configDefaults, defineConfig } from 'vitest/config'

// bodger's REST API always binds loopback (docs/architecture.md §9,
// ADR-0006 — no auth yet). `bodger serve`'s default is 127.0.0.1:8080;
// override with BODGER_DEV_API_PROXY_TARGET if you run it on another port.
const apiProxyTarget =
  process.env.BODGER_DEV_API_PROXY_TARGET ?? 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    // Serves `npm run dev` over real localhost HTTPS (self-signed by a
    // locally-trusted CA, via mkcert) rather than plain HTTP. Safari
    // doesn't reliably honor the session cookie's Secure attribute over
    // http://localhost — a real, currently-open WebKit inconsistency
    // (issue #86, #85) — so without this, Safari can't complete login
    // against the dev server at all.
    //
    // Its default `apply: 'serve'` correctly no-ops it out of
    // `make build-web` (a real `vite build`), but Vitest also resolves
    // this file through Vite's dev-server machinery, so `apply: 'serve'`
    // alone still triggers it there too — installing a local CA (an
    // interactive `sudo` prompt on first run) is not something a test run
    // should ever need or attempt. Vitest sets process.env.VITEST, so
    // exclude the plugin explicitly under it rather than relying on
    // `apply` alone.
    ...(process.env.VITEST ? [] : [mkcert()]),
  ],
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
    // root (`web/`) without this explicit opt-in.
    outDir: '../internal/platform/webui/dist',
    // false, deliberately: Vite's default empties outDir before every
    // build, which would also delete dist/.gitkeep — the tracked marker
    // that keeps internal/platform/webui's go:embed directive compiling
    // on a checkout that has never built the web UI. The Makefile's
    // build-web target does that same cleanup itself, excluding
    // .gitkeep, before invoking this build.
    emptyOutDir: false,
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // web/e2e/*.spec.ts (issue #65) are Playwright specs, not Vitest's —
    // they'd otherwise match Vitest's default *.spec.ts include pattern
    // and fail immediately (test() isn't valid outside a Playwright
    // runner). `npm run test:e2e` runs them instead.
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
})
