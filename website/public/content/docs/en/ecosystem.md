# ModernChat Ecosystem Architecture & Integration

> This document is the single entry point for cross-repo collaboration within the Modernchat project group: component positioning and relationships, login topology (including identity-space constraints), contract sources and golden samples, deployment combos, and release coordination. Each repo's internals are documented in its own repository; see the Design Doc and Backend API Spec for core-repo details.

## §1 Positioning: one core, four components

Modernchat-new is the project group's core (web chat platform); the other four repos are its components. Every component has a **dual nature**: it can be deployed standalone, or combined with the others through standardized contracts (REST / WS / JWT / OAuth) — no hard coupling prevents any component from running independently.

| Repo | GitHub | Stack | Positioning | Dev default ports (web / backend / DB) | Latest release |
|---|---|---|---|---|---|
| **Modernchat-new (core)** | modern-chat-new | Go 1.26 + Vue 3 monorepo | The chat platform itself: REST / WS / plugins / website / admin console | 5173, website 5174 / 8080 / 3306 | 0.12.0 |
| Modernchat-Hub | Modernchat-Hub | Go 1.26 + Vue 3 | Unified management panel: service registry & probing, update orchestration, unified admin backend, **OAuth Provider (unified login center)** | 5176 / 8091 / 3308 | 0.5.0 |
| Modernchat-sso | Modernchat-sso | Go 1.26 + Vue 3 | Standalone SSO identity service: register/login, email codes, admin user management; can serve as the main repo's identity source without Hub | 5175 / 8090 / 3307 | 0.1.0 |
| Modernchat-desktop | Modernchat-desktop | Tauri 2 + Vue 3 | Desktop client (QR login + minimal chat loop, tokens in OS keychain) | 1420 / — / — | 0.1.0 |
| Modernchat-mobile | Modernchat-mobile | Flutter | Mobile client (Android / iOS, auth / chat / contacts / settings) | — / — / — | 1.0.0+1 |

> Ports are development defaults for local alignment only. Randomized ports are safer in production (see the Ops manual); production values follow the deployment config.

## §2 Login & identity topology

The main repo supports three identity sources. **One main-repo instance should use exactly one identity source at a time** (see the warning below):

| Topology | Identity data | Integration | Typical scenario |
|---|---|---|---|
| A. Built-in accounts | main repo users table | Default, zero config | Minimal deployment (no Hub / sso) |
| B. Hub as unified login center | Hub users table | Set `SSO_HUB_URL` to the Hub plus `SSO_CLIENT_ID / SSO_CLIENT_SECRET` (register an OAuth client in the Hub admin); the main repo's SSO client needs zero code changes | Standard (full) deployment, multi-site unified accounts |
| C. sso as standalone identity service | sso users table | Same OAuth / SSO client contract as B — point `SSO_HUB_URL` at sso | Sites that skip Hub but still want auth separated |

**⚠️ uid-space constraint (first thing to check in cross-repo identity work)**: the group's JWT profile is uniform (HS256, claims only `uid` + `tv`); sharing the same `JWT_SECRET` makes tokens mutually trusted — **but only when both sides' uids point to the same user database**. Built-in accounts (uid from the main repo's users table) and topologies B / C (uid from Hub / sso user tables) are **different uid spaces**; mixing them makes one token resolve to different users in different services. Therefore:

- Configure exactly one identity source per main-repo instance; once `SSO_HUB_URL` is set, all authorization decisions follow tokens issued by that source — never mix uid semantics of locally registered users;
- Switching identity sources is a **migration**: stop writes on the old store → migrate users (preserve uids or build a mapping table) → switch over; never run two identity sources in parallel;
- Once `JWT_SECRET` is shared across services it must not be rotated casually — the main repo also derives SecretBox (admin credential encryption) and email-code HMAC from it; rotation requires the full procedure in the Security & Migration Guide.

## §3 Contract sources & golden samples

### 3.1 Source-of-truth index

| Contract | Source of truth | Consumers |
|---|---|---|
| REST shape | `docs/后端需求规格.md` §3 ↔ `web/src/api/index.ts` | All clients |
| WS frames | `web/src/types/proto.ts` (`ClientFrame` / `ServerFrame`) | desktop / mobile / plugins |
| Third-party client onboarding | `docs/客户端接入指南.md` | desktop / mobile / future clients |
| JWT / OAuth | Hub `/oauth/authorize` + `/api/oauth/{token,userinfo}` (contract-aligned with the main repo's SSO client) | main repo / sso |

### 3.2 Contract golden samples (`contracts/`)

`contracts/` holds the **machine-readable golden samples** of the cross-client contract: the main repo maintains the only source; desktop / mobile keep synchronized copies, and client tests consume the copies to assert "parsing matches the main repo" instead of "parsing matches ourselves".

| File | Content |
|---|---|
| `contracts/ws-frames.json` | Golden samples for WS `ClientFrame` / `ServerFrame`, field-by-field aligned with proto.ts |
| `contracts/rest-samples.json` | Golden samples for key REST responses (version negotiation / current user / conversations / message pagination) |

**Sync & verification rules**:

1. Change contracts only in the main repo (Backend API Spec first, then golden samples and proto.ts), then copy to the client-side copies;
2. Each copy lives next to a `*.sha256` fingerprint file; CI verifies "the copy was not hand-edited" — editing a copy without updating its fingerprint fails CI, preventing silent contract drift;
3. Client tests consume the copies: desktop `src/core/network/ws.test.ts`, mobile `test/contract_frames_test.dart` read the JSON and assert field semantics;
4. Golden samples are append-only in semantics: changing an existing field is a breaking change and requires version negotiation (§6) plus CHANGELOG entries on both ends.

## §4 Client capability matrix

Current implementation status per client (golden samples are the full contract; clients may lag):

| Capability | web | desktop | mobile |
|---|---|---|---|
| Core messaging loop (chat / typing / read / recall / edit) | ✅ | ✅ | ✅ |
| reactions / delivery receipts / group admin / search & favorites | ✅ | — | — |
| call.* voice / video signaling | ✅ | — | — |
| Version negotiation `GET /api/client/version` | ✅ | ✅ | ✅ |
| QR login | confirm side | initiator | confirm side |
| Client identity `X-Client-*` | ✅ | ✅ | ✅ |

Flow for a new frame: main-repo golden sample → copy to client repos that need it and implement → update this table.

## §5 Deployment combos

| Combo | Includes | Notes |
|---|---|---|
| Minimal | main repo | Built-in accounts, `install.sh` ready out of the box |
| Standard | main repo + Hub | Hub provides the unified login center (OAuth) plus centralized ops: version matrix / update orchestration / unified admin backend (both the main repo and sso are wired into Hub update adapters) |
| Full | main repo + Hub + sso + desktop + mobile | Pick **exactly one** identity source between Hub and sso (see §2); clients ship via their own release channels |

desktop / mobile are not server deployments: they learn about versions via the in-app negotiation endpoint; `upgradeRequired=true` only prompts, never hard-blocks, and old clients are never force-signed-out by the server.

## §6 Version & release coordination

- **Version negotiation**: each client requests `GET /api/client/version?platform=&clientType=&appVersion=` (unauthenticated) and receives `{version, minVersion, upgradeRequired, message?}`; client identity is reported via `X-Client-*` headers and WS query params, letting the server distinguish devices in the session list (see the Client Integration Guide).
- **Release order for cross-repo contract changes**: main repo first (golden samples + proto.ts + server in one commit) → clients follow (copies + implementation) → Hub adapters if affected. Docs and code ship in the same commit (group iron rule).
- **Hub version matrix**: the Hub probes health / version of registered service instances and orchestrates updates — main repo `GET /api/health` + `/api/version`, sso `GET /healthz` + `/api/v1/version`; both are wired into Hub update adapters. desktop / mobile versions ship via their own releases, not through the Hub.
- **CHANGELOG**: each repo maintains its own; entries that touch cross-repo contracts must appear in both repos' CHANGELOGs.

## §7 Collaboration conventions

1. **API changes**: update `docs/后端需求规格.md` first → then the backend and `proto.ts` (plus golden samples) → finally the web frontend and clients;
2. **Docs and code in the same commit**: features / architecture → Design Doc; APIs → Backend API Spec; env vars / deployment → Ops manual; user-visible changes → CHANGELOG `[Unreleased]`;
3. **Commit messages**: Conventional Commits with Chinese descriptions (`feat(adapter): xxx`); after editing Chinese docs run `website/scripts/sync-docs.ps1` to refresh the website mirror, and update the English mirror by hand;
4. **Security red lines**: never commit credentials; server logs never contain passwords / tokens / message bodies; `FRONTEND_URL` (comma-separated) doubles as the WS Origin whitelist — every client or panel domain must be registered there or the WS handshake is rejected.
