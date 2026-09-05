# bodger web UI

React + TypeScript + Vite, built to static assets embedded in the `bodger` binary. See [`docs/architecture.md`](../docs/architecture.md) §3-5 for how this surface fits into the rest of the project, and [`docs/decisions/0001-technology-stack.md`](../docs/decisions/0001-technology-stack.md) for why.

## Commands

Run these from the repository root via `make` (`make setup`, `make build`, `make lint`, `make test`) so Go and web targets stay in lockstep — see the root [`Makefile`](../Makefile). From this directory directly:

| Command                             | What it does                                                                                                               |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `npm run dev`                       | Vite dev server with hot reload, proxying `/api` and `/healthz` to a locally running `bodger serve` (see `vite.config.ts`) |
| `npm run build`                     | Type-check, then build static assets into `internal/platform/webui/dist` — what `go build` embeds                          |
| `npm run lint`                      | [oxlint](https://oxc.rs)                                                                                                   |
| `npm run fmt` / `npm run fmt:check` | [Prettier](https://prettier.io), write or check                                                                            |
| `npm run test`                      | [Vitest](https://vitest.dev) component tests                                                                               |

To iterate on the UI against a real backend: `go run ./cmd/bodger serve` in one terminal, `npm run dev` in this directory in another, then open the Vite dev server's URL (not `bodger serve`'s).

## Layout

- `src/routes.tsx` — the route tree; `src/App.tsx`'s `AppLayout` is the shared shell every route (other than `/login`) renders inside.
- `src/pages/` — screens. Only `Placeholder.tsx` exists so far; the real screens land in issues #59-#63.
- `src/components/ui/` — [shadcn/ui](https://ui.shadcn.com) components: Radix primitives styled with Tailwind, copied into the repo rather than installed as a themed component library. Add more with `npx shadcn@latest add <component>`.
- `src/lib/utils.ts` — `cn()`, shadcn's class-merging helper.

## What this layer must never do

Per `docs/architecture.md` §3: no client-side balance summation, currency conversion, period arithmetic, or category rollup. This UI renders what the REST API computed — formatting a server-supplied number for display is fine, deciding what that number _is_ is not.

<!-- CI isolation test (#109): a single web/-only commit against the ci/split-jobs-and-cache-playwright base should skip check-go and hit the Playwright cache. Throwaway PR, not to be merged. -->
