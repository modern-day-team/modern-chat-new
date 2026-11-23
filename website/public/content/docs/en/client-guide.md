# Client Integration Guide

> Applies to: Web (`web/`), the Tauri 2 desktop app (`Modernchat-desktop`), the Flutter mobile app (`Modernchat-mobile`), and SSO / Hub components that call the main site's APIs.
> The API contract is defined in `docs/api-spec.md` (API Specification); this document covers **cross-platform conventions and pitfalls only**.

## 1. Server address and origin registration

All clients use the same backend: REST prefix `/api`, WebSocket `/ws`, uploads `/uploads`.

- Clients with a browser engine (Web, Tauri WebView) send an `Origin` header. Each origin must be listed in the server's `FRONTEND_URL` (comma-separated), otherwise **the WS handshake is rejected and cross-origin REST calls are blocked by the browser**. Typical values:
  - Web dev `http://localhost:5173`; production `https://chat.example.com`
  - Tauri dev `http://localhost:1420` (the `devUrl` in `tauri.conf.json`)
  - Tauri production `tauri://localhost` / `https://tauri.localhost` (Windows uses the latter)
  - Flutter Web debug ports as applicable
- Native clients without an `Origin` (Flutter Android/iOS via Dio, Go programs) are not subject to the origin whitelist, but still need a valid JWT.
- `FRONTEND_URL` is also used as the password-reset link prefix — don't list only one address and break links for other platforms.

## 2. Login and tokens

- Password login: `POST /api/auth/login {username,password}` → `{token,user}`; with 2FA enabled it first returns `{twofa:true,ticket,method}`, then `POST /api/auth/2fa/verify {ticket,code}` for the token.
- Desktop apps should use QR login: `POST /api/auth/qr/start` renders a QR code → poll `POST /api/auth/qr/status` → receive the token on `confirmed`. The mobile side calls `POST /api/auth/qr/confirm`. Tickets are single-use; poll with a POST body so the token never enters access logs.
- SSO flow: `GET /api/auth/sso/start?redirect=` → hub → `callback` → `POST /api/auth/sso/exchange {ticket}`.
- Token storage:
  - **Always call REST with `Authorization: Bearer <token>`.** REST only accepts Bearer (not cookies, to avoid CSRF); the `mc_auth` cookie is only for browser media loading (`/uploads`) and WS fallback.
  - Web: `localStorage` (key `mc_token`). Desktop/mobile: platform secure storage (Tauri keychain plugin or encrypted file, Flutter `flutter_secure_storage`). **Never write tokens to logs, crash reports, or URLs.**
  - JWT lasts 7 days. Password change / reset / ban / `logout-others` invalidate `token_version`; on 401 clear the local token and go to login.
- Logout: `POST /api/auth/logout` only clears the cookie and does not revoke the JWT. Use `POST /api/me/logout-others` for global sign-out, or `DELETE /api/me/sessions/:id` to sign out one device.

## 3. Client identity and sessions

- Authenticated requests may send these headers (optional, ≤32 chars, invalid values ignored):
  - `X-Client-Platform`: `web` | `desktop` | `mobile` | `tablet` | `other`
  - `X-Client-Type`: subtype such as `tauri` / `flutter-android` / `flutter-ios` / `browser`
  - `X-Client-Version`: app version, e.g. `0.1.0`
- Every JWT contains a 128-bit session ID (`sid`). The server records it on first request using the headers above, then only refreshes `lastSeenAt`. `GET /api/me/sessions` lists sessions; `DELETE /api/me/sessions/:id` revokes one (the token is rejected immediately and its WS is disconnected).
- WS handshakes also accept `?platform=&clientType=&appVersion=` for flows like QR login where the token arrives before the client identity.
- Show a session list in settings (L3, low-frequency, owner-only): platform, version, IP, last seen, and a "sign out this device" action.

## 4. WebSocket realtime channel

- Connect to `GET /ws?token=<JWT>`; cookie fallback is for browsers only. Browser origins must be in `FRONTEND_URL`.
- Send `{"type":"ping"}` every 25s; server replies `pong` (read timeout 60s).
- Frame shapes are defined by `ClientFrame` / `ServerFrame` in `web/src/types/proto.ts` — **the single source of truth**; align your types field by field, don't guess.
- Idempotency: always include `clientMsgId` (UUID) with text messages; after reconnect use `/api/conversations/:id/messages?afterId=` for incremental catch-up.
- A `kicked` frame means forced sign-out (ban / password reset / session revoked): clear the token and return to login.
- Keep stale data during background refresh; apply WS events locally first, prefer targeted updates over full refetches.

## 5. Version negotiation

`GET /api/client/version?platform=&clientType=&appVersion=` (public) returns `{version,minVersion,upgradeRequired,message?}`. Call it at startup; when `upgradeRequired=true`, prompt the user to upgrade (never hard-block, to avoid mass lockout during offline releases). `minVersion` comes from `MIN_CLIENT_VERSION`; when unset it is always false.

## 6. Uploads and attachments

- Upload via `POST /api/upload?kind=image|audio|video|file|sticker`, multipart field `file`; do not set `Content-Type` (let the runtime add the boundary).
- Reading `/uploads/...` requires login: browsers rely on the same-origin HttpOnly cookie for `<img>/<video>`; native clients use Bearer. **Never lazy-load message images** (virtual list measurement depends on real height).
- The server detects real file types from headers; compress on the client to save bandwidth (Web images: max edge 1600 / webp).

## 7. Multi-client consistency checklist

1. REST shapes match `docs/api-spec.md` §3.2; errors are always `{"error":"Chinese message"}`; 401 always clears the token.
2. WS frames match `proto.ts` field by field; `clientMsgId` is idempotent; reconnect uses incremental catch-up.
3. i18n keys are added to both Chinese and English; server Chinese errors are mapped via `errors.ts`.
4. UI entry points follow the three-layer information architecture (L1 main / L2 detail panel / L3 settings); classify before adding a new entry.
5. After a release, smoke-test: login → conversation list → send message → refresh → reconnect → attachment load.
