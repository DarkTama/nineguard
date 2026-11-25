# CORS for the /v1 proxy

## Why

A browser web app (promptpaste, hosted on its own subdomain, e.g. `https://<app-domain>`) needs to call
`POST /v1/chat/completions` and `GET /v1/models` on `https://<gateway-domain>` directly with
`Authorization: Bearer sk-ng-...`.

Today NineGuard sends no CORS headers at all, so browsers block these requests. Native clients (Chatbox, Pi, Cursor)
are unaffected because they don't enforce CORS.

## Goal

Browsers on an allow-listed origin can call `/v1/*` with a Bearer key. Nothing else changes.

## Design

1. **Config:** new env var `NINEGUARD_CORS_ORIGINS` in `internal/config/config.go`
   (comma-separated exact origins, e.g. `https://<app-domain>,http://localhost:5173`).
   Empty (the default) = CORS disabled, behaviour identical to today.
2. **Middleware** wrapping the outermost handler in `cmd/nineguard/main.go` (outside
   `authMgr.RequireAuthMiddleware`, line ~428), applied only to paths `/v1`, `/v1/*`, `/v1beta/*`:
   - If `Origin` header exactly matches an allowed origin:
     - set `Access-Control-Allow-Origin: <that origin>` and `Vary: Origin`.
   - If the request is a preflight (`OPTIONS` with `Access-Control-Request-Method`):
     - respond `204` immediately, **before auth and before the reverse proxy**, with
       `Access-Control-Allow-Methods: GET, POST, OPTIONS`,
       `Access-Control-Allow-Headers: Authorization, Content-Type`,
       `Access-Control-Max-Age: 600`.
   - Origin not allowed: add no CORS headers, pass through unchanged (browser blocks it).
3. **No credentials:** do not send `Access-Control-Allow-Credentials`. Auth is a Bearer header, not cookies.
   Never use `*` together with the admin UI / `/api/v1/*` (those stay without CORS).
4. **Streaming:** headers must be set before the proxy writes, so SSE responses also carry them.

## Constraints

- Admin API (`/api/v1/*`), web UI, and `/portal` must not get CORS headers.
- Upstream responses that already carry `Access-Control-*` headers: strip/overwrite them on `/v1` so the browser
  sees exactly one value.

## Verification

- Unit test for the middleware: allowed origin preflight → 204 + headers without hitting auth; disallowed origin →
  no headers; `/api/v1/...` with allowed origin → no headers; empty config → no headers anywhere.
- Manual: from the browser console on an allowed origin,
  `fetch('https://<gateway-domain>/v1/models', {headers:{Authorization:'Bearer sk-ng-...'}})` succeeds.
- Deploy: set `NINEGUARD_CORS_ORIGINS` in Dokploy env for the NineGuard service.
