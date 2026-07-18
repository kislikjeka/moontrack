---
name: verify-ui
description: Drive a headless browser via the Playwright MCP to visually verify a frontend change before claiming it works. Use whenever you have just edited files under `apps/frontend/src/`, or when the user asks to "verify the UI", "check the page", "see if it renders", or "screenshot the X view". Default to reading the accessibility tree for assertions; only screenshot when the question is visual (layout, overlap, spacing). Never report a frontend change as complete without running this skill.
---

# verify-ui

Visually verify a MoonTrack frontend change by driving a real headless browser (Playwright MCP) against the running dev loop — instead of guessing from the code that a change renders correctly.

## Preconditions

1. **Dev loop is running.** Per the project setup, Postgres/Redis/backend run in Docker and the frontend runs on the host via Vite. Do **not** try to start any of them yourself — start via `just dev` is the user's job. Check first:

   ```bash
   curl -sf http://localhost:5173 > /dev/null && curl -sf http://localhost:8080/health/live > /dev/null
   ```

   Frontend is Vite on **:5173**, backend is on **:8080**. Health checks (`/health`, `/health/live`) live at the **root**; the app API lives under **`/api/v1`** (auth, wallets, transactions, …). Vite proxies `/api` → `http://localhost:8080`. If either port is down, stop and tell the user to run `just dev`.

2. **`.mcp.json` exists at the repo root** with the Playwright MCP entry below, and `playwright` is listed in `.claude/settings.local.json` → `enabledMcpjsonServers`. If absent, stop and ask the user to add it (or enable the MCP server in the Claude Code UI). Do not edit `.mcp.json` yourself unless explicitly told to.

   ```json
   {
     "mcpServers": {
       "playwright": {
         "command": "npx",
         "args": [
           "-y", "@playwright/mcp@latest",
           "--browser", "chromium",
           "--headless",
           "--allowed-origins", "http://localhost:5173;http://localhost:8080"
         ]
       }
     }
   }
   ```

## Workflow

MoonTrack auth is **JWT-based**, not cookie-based. The frontend (`apps/frontend/src/services/api.ts`) reads the token from `localStorage['auth_token']` on every request via an axios interceptor and sends it as `Authorization: Bearer <token>`. `getCurrentUser()` reads `localStorage['user']`. So to get an authenticated session you must **register a throwaway user via the API, then seed both localStorage keys on the frontend origin** before navigating to a protected route. Simply navigating won't be authenticated (there's no cookie).

1. **Register a throwaway user** to get a JWT. The register endpoint returns `201` with `{ token, user }`. Password must be ≥ 8 characters.

   ```js
   const email = `agent-verify-${Date.now()}@local.test`;
   const res = await page.request.post('http://localhost:8080/api/v1/auth/register', {
     data: { email, password: 'verify-password-12345' },
     headers: { 'Content-Type': 'application/json' }
   });
   const { token, user } = await res.json();
   ```

2. **Seed the token into the frontend origin's localStorage**, then navigate. The cleanest way is an init script so it's present before app code runs:

   ```js
   await page.addInitScript(([token, user]) => {
     localStorage.setItem('auth_token', token);
     localStorage.setItem('user', JSON.stringify(user));
   }, [token, user]);

   await page.goto('http://localhost:5173/dashboard');
   ```

   Now every request the app makes carries the Bearer token, and `getCurrentUser()` sees the user (no redirect to `/login`). If the app ever bounces you to `/login`, the token wasn't seeded on the right origin — re-check step 2 runs before `goto`.

3. **Navigate to the page that exercises your change.** Protected routes: `/dashboard`, `/wallets`, `/wallets/:id`, `/transactions`, `/transactions/new`, `/transactions/:id`, `/settings`. (`/` and unknown paths redirect to `/dashboard`.)

4. **Assert via the accessibility tree, not screenshots.** Use `browser_snapshot` or Playwright's `getByRole` / `getByText` and let auto-waiting handle async loads (TanStack Query fetches) — never insert sleeps.

   ```js
   await expect(page.getByRole('button', { name: 'Add wallet' })).toBeVisible();
   ```

5. **Take a screenshot only if the question is visual** — overlap, layout, spacing, theme (light/dark), chart rendering (Recharts). DOM assertions cannot answer "does the PnL badge sit correctly next to the asset row?" or "is the portfolio chart clipped?".

### Fresh account = empty portfolio

A throwaway user has **no wallets and no transactions**, so protected pages render their empty states. That's the right target when verifying empty/zero states, layout chrome, nav, and forms. To verify populated views (portfolio totals, transaction lists, charts with data), either:

- Create data through the UI you're testing (add a wallet via `/wallets`, add a transaction via `/transactions/new`), or
- POST directly to the protected API with the Bearer token (e.g. `POST /api/v1/wallets`, `POST /api/v1/transactions`) to seed fixtures, then reload.

Prefer driving the real UI when the flow under test *is* that create flow; use the API only to set up preconditions for a different view.

## Hard rules

- **Don't claim a frontend change works without running this skill.** "Tests pass" (`bun test`) is not visual verification.
- **Don't start `just dev` yourself.** Tell the user if a port is down.
- **Seed localStorage before `goto`, not after.** `addInitScript` runs before app code; setting localStorage after navigation means the first render already redirected to `/login`.
- **Don't screenshot what the accessibility tree can answer.** Screenshots cost tokens and rot fast — reserve them for genuinely visual questions.
- **Don't use `waitForTimeout` / `sleep`.** Use `expect(...).toBeVisible()` or `waitFor` — data loads are query-driven, real waits are deterministic.
- **Don't reuse emails across runs.** `Date.now()` in the email is load-bearing for repeatability (register is idempotent-hostile: a duplicate email returns `409`).
- **Don't add backend endpoints or DB fixtures.** `POST /api/v1/auth/register` plus the existing protected endpoints are sufficient to set up any state.
