# Embedded CLIProxyAPI OAuth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add account-based Claude and Codex OAuth providers to NineGuard using an embedded CLIProxyAPI engine, with encrypted token lifecycle management and a redesigned Providers page with selectable icons.

**Architecture:** NineGuard keeps its existing admission, plugins, quota, routing and traffic pipeline. A private, authenticated loopback CLIProxyAPI SDK service handles account-backed execution and provider-specific OAuth. Existing custom HTTP providers bypass the SDK; all provider credentials persist through the NineGuard vault/SQLite store.

**Tech Stack:** Go 1.27.1, CLIProxyAPI `/v8` v8.0.43 pinned to commit `307803510bce87f986fcf7b1a84190f105c7b4aa`, SQLite (one connection), existing AES-256-GCM vault, embedded vanilla JS/CSS.

**Spec:** `docs/superpowers/specs/2026-10-26-embedded-provider-oauth-design.md`

## Global Constraints

- No imports of upstream `internal/*` packages.
- No SDK default plaintext token store, token export endpoint, or direct serialization of upstream `Auth` objects.
- Existing custom upstream providers keep their current HTTP path; they do not route through the SDK merely because the SDK is installed.
- Management operations require NineGuard administrator authorization, not a client key or an operator session. Existing unrelated provider permissions are intentionally unchanged.
- SQLite has one connection: close rows before additional queries, use only tx within transactions, and never make network requests while holding a database transaction or manager lock.
- Account ids survive refresh and reconnect. Several accounts can be attached to one Provider and are visible individually.
- Do not silently switch to an unrelated Provider/account when the selected Provider cannot serve a model.
- No anti-ban guarantee, account-limit evasion, or identity-spoofing feature is part of this design.
- Preserve the existing CRLF convention where present and unrelated edits already on `feat/provider-icons`.
- Use existing palette and bundled NineGuard Mono; no decorative gradients, status dots or repeated stat cards.
- No additional permanent UI wiring/source-text tests. Existing JS suites and real browser interaction verify the UI.

## Review Focus

- Restart restores tokens but not models: explicitly publish restored accounts through the SDK post-persist hook (Task 3).
- OAuth tokens live in `Auth.Storage`, which is `json:"-"`: encrypting `json.Marshal(auth)` alone loses credentials (Task 2).
- A refresh or callback completing after disconnect must not recreate the account; tombstones/version fences reject the write (Tasks 2 and 4).
- Two Providers of the same family must never share account selection or catalogs accidentally (Task 5).
- Remote dashboard users cannot receive the server's loopback callback automatically: manual callback submission remains available and owner-bound (Tasks 4 and 7).

## Verified scratch evidence

A scratch module resolved the pinned commit to v8.0.43, built with `CGO_ENABLED=0` and ran the actual SDK service. It used a custom AES-GCM encrypted in-memory Store, an empty `WatcherWrapper`, and a captured Gin router via public `sdk/api.WithRouterConfigurator`.

Observed: `models=38 store=encrypted runtime_credential=true router_captured=true`; clean shutdown; only `config.yaml` was created, no token files. This used fake credentials and did not authenticate or infer against a real provider.

Two integration constraints were discovered:
1. `coreManager.Load` alone does not publish the restored account's model catalog with an empty watcher. The public management `PATCH /v0/management/auth-files/fields` invokes the service's post-persist runtime synchronization hook. A patch setting the account's internal prefix published its models immediately. Wrap this route internally, and use the real login post-persist hook for newly authenticated accounts.
2. `WithLocalManagementPassword` alone still returned `remote management key not set`. Set an ephemeral configured management secret as well; do not expose management externally.

## File Structure and Shared Contracts

- Extend `internal/providers/providers.go` / `internal/db/db.go`: persistent Provider `Backend` (`http` or `cliproxy`), `Family`, existing `Icon`. Existing rows default to `http`; SDK routes are generated, not user-editable.
- New `internal/providerengine/types.go`: engine-only safe contracts, no HTTP handler or DB implementation.
- New `internal/providerengine/store.go`, `credentials.go`: encrypted SDK Store and stable account metadata.
- New `internal/providerengine/engine.go`, `catalog.go`, `oauth.go`: SDK lifecycle, scoped execution/model catalogs, OAuth sessions.
- New `internal/handler/provider_oauth.go`: explicitly registered administrator endpoints; no wildcard proxy to SDK management.
- Modify `internal/proxy/proxy.go`, `internal/providers/providers.go`, `cmd/nineguard/main.go`: engine lifecycle, model discovery and post-admission dispatch.
- New `web/static/js/providericons.js`, `web/static/js/provideroauth.js`, `web/static/provider-icons/*`: identity and login UI; refactor existing `web/static/js/views/providers.js` and page-scoped CSS in `web/static/styles/main.css`.

Public NineGuard types (defined in `internal/providerengine/types.go`):
- `Account`: `ID`, `ProviderID`, `Family`, `Label`, `Status`, `ExpiresAt`, `LastRefreshedAt`, `LastError`; no credentials, SDK object, file path or internal listener address.
- `LoginSession`: `ID`, `ProviderID`, `AccountID`, `State`, `AuthorizationURL`, `UserCode`, `VerificationURL`, `ExpiresAt`, `Error`; only applicable fields populated.
- `Connector`: `ID`, `Label`, `Protocols []string`, `LoginModes []string`, `Icon`.
- `Engine.New(database *db.DB, dataDir string) (*Engine, error)`.
- `(*Engine).Start(ctx context.Context) error`, `Close(ctx context.Context) error`.
- `(*Engine).Connectors() []Connector`, `Accounts(ctx context.Context, providerID string) ([]Account, error)`.
- `(*Engine).StartLogin(ctx context.Context, adminID int64, providerID, accountID, mode string) (LoginSession, error)`.
- `(*Engine).LoginStatus(ctx context.Context, adminID int64, providerID, sessionID string) (LoginSession, error)`.
- `(*Engine).SubmitCallback(ctx context.Context, adminID int64, providerID, sessionID, callbackURL string) error`.
- `(*Engine).CancelLogin(ctx context.Context, adminID int64, providerID, sessionID string) error`.
- `(*Engine).Refresh(ctx context.Context, providerID, accountID string) error`, `Disconnect(ctx context.Context, providerID, accountID string) error`.
- `(*Engine).Models(ctx context.Context, providerID string) ([]map[string]interface{}, error)` returns unprefixed upstream model ids; the existing providers manager adds the public prefix.
- `(*Engine).RoundTrip(ctx context.Context, providerID, accountID string, req *http.Request) (*http.Response, error)` accepts the prepared upstream body, chooses only credentials belonging to that Provider, uses an internal model namespace and returns the client protocol. Empty accountID allows selection only within the Provider.

### Task 1: Persistent provider execution kind and identity

**Files:** modify `internal/db/db.go`, `internal/providers/providers.go`, `internal/providers/providers_test.go`, `internal/handler/handler.go`; use existing uncommitted `internal/providers/icon.go`.

**Interfaces:** produces Provider fields `Backend string`, `Family string`, `Icon string`; `ProviderOptions` gets corresponding optional update fields. Preserve omitted identity fields on update. Old wrappers continue to mean HTTP providers and preserve stored identity on editing.

- [ ] Write `TestProviderBackendMigrationAndRoundTrip`: opening an old DB twice preserves HTTP routing and data; a `cliproxy/claude` Provider round-trips through create/get/list/update; arbitrary SDK route URLs, unknown backend/family, and incompatible family mutation with connected accounts are rejected. Icon presets/custom URL persistence and invalid schemes retain their existing regression test.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/providers ./internal/db -run 'TestProvider(Backend|Icon)'`; observe expected failure before implementation.
- [ ] Implement idempotent column/table migrations and validation. SDK identity is explicit, never inferred from prefix or icon. Provider create/update/get/list do not query another table while rows remain open.
- [ ] Run the same tests; expect PASS. Commit on the feature branch: `feat(providers): persist provider execution kind and identity`.

### Task 2: Encrypted credential store and account lifecycle fences

**Files:** create `internal/providerengine/types.go`, `credentials.go`, `store.go`, `store_test.go`; modify `go.mod`, `go.sum`, `internal/db/db.go`.

**Interfaces:** consumes Task 1 Provider identity; produces SDK `coreauth.Store` implementation and safe Account DTOs. Tables: `provider_accounts` (stable identity, Provider binding, status, version, disconnect tombstone) and `provider_credentials` (account id, encrypted payload, SDK registration/credential/generation counters). Pending login ownership is in memory and expires/cancels at restart.

- [ ] Write `TestCredentialStoreEncryptedRestartAndStoragePayload`: a fake SDK `TokenStorage` exposes access/refresh token JSON while `Auth.Metadata` contains only email; Save+new Store+List restores both token fields in executable metadata, raw SQLite and Account JSON contain neither token, corrupted ciphertext returns an error.
- [ ] Write `TestCredentialStoreDisconnectAndVersionFences`: stale refresh cannot overwrite a newer token, Save after disconnect cannot recreate it, and reconnect is admitted only by an explicit owner-bound creation grant. Stable account id must remain unchanged.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/providerengine -run TestCredentialStore`; observe failure.
- [ ] Implement the Store. Merge serialized `Auth.Storage` fields and runtime metadata into an encrypted envelope without calling `SaveTokenToFile`. Never flatten secret storage into Account DTOs. Preserve SDK version counters; use immutable metadata replacements. Handle all vault errors; persist via short transactions and version checks, never network calls under tx/locks. Return a synthetic non-filesystem locator (`sqlite://provider-accounts/<id>`) to upstream hooks.
- [ ] Run tests; expect PASS. Commit: `feat(providers): encrypt OAuth credentials with lifecycle fencing`.

### Task 3: Embedded engine startup and account publication

**Files:** create `internal/providerengine/engine.go`, `catalog.go`, `engine_test.go`; modify `cmd/nineguard/main.go`.

**Interfaces:** produces `New`, `Start`, `Close`, `Connectors`, account-scoped model catalog and internal publication helper. Uses public SDK packages only; register the custom store before creating management handlers/service.

- [ ] Write `TestEmbeddedEnginePublishesRestoredAccounts`: seed fake Claude and Codex records into encrypted Store, start actual SDK, verify each account has published catalog models; stop/start preserves identities/catalogs; no token JSON file exists. Readiness requires a successful authenticated request, not the SDK's timed OnAfterStart notification.
- [ ] Write `TestEnginePrivateSurface`: engine port binds only 127.0.0.1, missing/wrong internal secret cannot execute or manage credentials, no NineGuard API returns engine credentials/addresses. Startup bind failure must fail readiness and stop workers.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/providerengine -run 'TestEmbeddedEngine|TestEnginePrivate'`; observe failure.
- [ ] Pin SDK v8.0.43. Build using the verified empty WatcherWrapper/custom Store path, capture Gin router, configure random inference and management secrets, disable SDK control panel, file payload logging, discovery and arbitrary plugins. Use public internal metadata-patch route to publish restored records; invoke SDK hooks for login publication. Check encryption/persistence before claiming connected. Normalize `context.Canceled` during deliberate shutdown; close engine before DB.
- [ ] Run tests and real local executable smoke; expect catalog publication and clean shutdown. Commit: `feat(providers): embed private CLIProxyAPI execution engine`.

### Task 4: Owner-bound OAuth sessions and account management API

**Files:** create `internal/providerengine/oauth.go`, `oauth_test.go`, `internal/handler/provider_oauth.go`, `provider_oauth_test.go`; modify `cmd/nineguard/main.go`.

**Interfaces:** produces login and account methods listed above. Register only these NineGuard routes under global auth, each with administrator check:
`GET /api/v1/provider-connectors`, `GET /api/v1/providers/{id}/accounts`, `POST /api/v1/providers/{id}/oauth/sessions`, `GET /api/v1/providers/{id}/oauth/sessions/{sessionId}`, `POST .../{sessionId}/callback`, `DELETE .../{sessionId}`, `POST /api/v1/providers/{id}/accounts/{accountId}/refresh`, `DELETE .../{accountId}`. Start body: `{account_id?, mode}`; callback body: `{callback_url}`. Explicit account enable/disable uses `PATCH .../accounts/{accountId}` with `{disabled: bool}`. All return safe DTOs.

- [ ] Write `TestOAuthSessionOwnershipAndReplay`: wrong user/Provider/family/state, expiry, duplicate completion and a cancelled session cannot attach credentials; admin-only routes reject client keys/operators. Invalid callback URLs cannot redirect users or leak authorization codes.
- [ ] Write `TestOAuthCompletionAndDisconnectRace`: simulate provider responses at the transport boundary using fake credentials; execute the real SDK login/persist/publication hooks; assert the account and catalog appear once, reconnect keeps id and prefix, and disconnect while exchange/refresh is pending prevents resurrection.
- [ ] Write `TestRefreshStatusAndSecretRedaction`: transient failure retains credentials; invalid grant becomes needs_login; public errors and captured SDK logging contain no access/refresh/id token, authorization code or PKCE verifier.
- [ ] Run targeted tests before implementation; expect failure.
- [ ] Implement a bounded session manager with cryptographically random public ids, server-side state binding, and allowed connector modes. Inject an internal session header into SDK RequestInfo; the pre-persist hook validates the session, rewrites stable identity/Provider namespace and admits Store creation. Never forward that header to real providers. Connect via SDK management handler, not by importing private auth services. Browser opens auth URL client-side; loopback callback uses SDK listener, remote use submits the full returned callback URL manually through the owner-bound endpoint. Callback scratch files, where required by SDK, live in a 0700 ephemeral directory, are 0600 and deleted on completion/cancel/expiry/shutdown; token files remain forbidden. Register post-persist runtime publication immediately. Disconnect cancels associated sessions, tombstones Store and removes SDK auth/catalog entries.
- [ ] Verify each potential connector (Claude, Codex, Antigravity, Kimi, xAI, Meta, Devin) through its actual exported login, refresh and executor paths. `Connectors()` exposes only proven combinations; Claude and Codex browser login are required. Expose device login only where exported SDK APIs make it implementable; do not advertise an unimplemented device mode. Escalate any required unsupported flow rather than replacing OAuth with API keys.
- [ ] Run tests; expect PASS. Commit: `feat(providers): add secured OAuth login and account lifecycle API`.

### Task 5: Scoped models and post-admission SDK execution

**Files:** modify `internal/providers/providers.go`, `internal/proxy/proxy.go`, `cmd/nineguard/main.go`; create `internal/providerengine/transport.go`, `transport_test.go`, `internal/proxy/oauth_provider_test.go`.

**Interfaces:** engine provides `Models` and `RoundTrip` from shared contracts; providers manager consumes an engine-catalog interface set at application startup. Proxy receives an engine-dispatch interface; HTTP providers still call their existing transport.

- [ ] Write `TestOAuthProviderIsolationAndAdmission`: two same-family Providers cannot select each other's accounts/models; forbidden/disabled/exhausted requests never hit SDK; public model id preserves client prefix while internal namespace never reaches traffic/API DTOs. Missing usable account returns format-aware upstream error, not another Provider.
- [ ] Write `TestOAuthBufferedAndStreamUsage`: actual SDK runtime plus a fake upstream server exercises chat/messages/responses protocols, tool use and SSE; usage is metered once; client cancellation stops upstream. Existing custom HTTP pass-through remains unchanged.
- [ ] Run `CGO_ENABLED=0 go test -tags server ./internal/providerengine ./internal/proxy ./internal/providers -run 'TestOAuthProvider|TestOAuthBuffered'`; observe failure.
- [ ] Implement scoped selection using unique internal Provider prefixes and `ForceModelPrefix`, or a validated Provider-owned pinned account via public `handlers.WithPinnedAuthID`. Do not accept public pin/engine headers. Strip client auth, cookies and private routing headers before engine dispatch; carry only required protocol and request metadata. Use existing quota/admission/plugin/response-metering code; no duplicated traffic write. Query registry models only for accounts bound to the Provider and publish public prefix using existing canonical routing rules. No inferred Gemini-native support without native handler/metering coverage.
- [ ] Run targeted tests and a throwaway real server request through NineGuard→embedded SDK→local fake upstream; observe the response, recorded model/usage and cancellation. Commit: `feat(proxy): route admitted OAuth provider requests through embedded SDK`.

### Task 6: Provider icons and connection-oriented page layout

**Files:** create `web/static/js/providericons.js`, `web/static/provider-icons/*`; refactor `web/static/js/views/providers.js`; modify `web/static/styles/main.css`, `README.md`, `docs/PROVIDERS.md`.

**Interfaces:** reads Provider backend/family/icon and Connectors DTOs; produces `providerMark(provider)` DOM and icon-picker value (auto/preset/http(s) URL). Keep frontend preset ids aligned with server validation.

- [ ] Read frontend-design and antislop-ui plus available antislop core before UI work. Retain spec palette/type. Place icon/name/prefix at the start of each compact Provider section, connection method and account/URL next, health and explicit actions last. Move routing tutorial to a collapsed explanation.
- [ ] Implement create/edit form sections for provider family, authentication, routing and optional quota/token-saving settings. Custom URL retains API key fields; OAuth uses Connect account. Preset icons include 9router, OmniRoute, NineGuard, Claude, Anthropic, OpenAI, OpenRouter, Ollama, Gemini, generic; verify brand asset identity/license, do not invent logos. Bundle SVG as image assets, never untrusted innerHTML. Custom image uses HTTP(S), no referrer; fallback is generic/initial without error loops. Explain remote image privacy.
- [ ] Browser smoke with scratch DB: icon preset/custom URL create/edit/restart, broken image fallback, disabled/default provider, empty/load/error states, long URL/name, 390px and desktop, light/dark, keyboard-only. No live production provider mutations.
- [ ] Run `node --check` on edited JS and existing JS suite. Rebuild/restart embedded web assets before browser observation. Commit: `feat(ui): redesign provider connections and configurable icons`.

### Task 7: OAuth login and account token management UI

**Files:** create `web/static/js/provideroauth.js`; modify `web/static/js/views/providers.js`, `web/static/styles/main.css`, `docs/PROVIDERS.md`.

**Interfaces:** consumes Task 4 API; `openProviderLogin(provider, account?, onChanged)` opens owner-bound flow, and account panel displays only safe metadata.

- [ ] Implement browser authorization with user-triggered popup/link, pending/cancel/expired/failed/success states, supported device-code controls, copy/open authorization URL, and manual callback URL submission for remote use. Refresh account metadata after completion; cancel polling on dialog close/navigation and session on explicit Cancel. No token paste/export controls.
- [ ] Account rows show label, family, status, expiry, last refresh and sanitized error; actions Refresh, Login again, Enable/Disable, Disconnect. Confirm disconnect as forgetting the local credential, not guaranteed upstream revocation. Account actions remain tied to Provider and account id.
- [ ] Browser smoke using local provider test transport: connect→catalog available without restart, reconnect with stable id, refresh failure→needs_login, cancel and expired sessions, remote callback submission, disconnect during pending request, two Providers of same family. Verify 390px/desktop and both themes, focus/keyboard labels and no horizontal overflow.
- [ ] Run JS syntax and suite; report real-account verification separately if owner has not completed consent. Commit: `feat(ui): manage OAuth accounts from provider connections`.

### Task 8: Integration verification and documentation cutover

**Files:** update `docs/superpowers/specs/2026-10-26-embedded-provider-oauth-design.md`, this plan, `docs/ARCHITECTURE.md` (Indonesian), `docs/FUTURE_PLANNED_FEATURES.md`, `README.md`, `GLOSSARY.md`; add one ADR under the next available number for embedded execution/protocol boundary.

- [ ] Run `CGO_ENABLED=0 go test -tags server ./...`, `CGO_ENABLED=0 go build -tags server ./...`, `node --test web/jstest/*.mjs`, and `node --check` each edited JS. Exercise the actual scratch NineGuard binary through browser and buffered/SSE client requests; verify DB writes and credential absence in public/log output. Perform a race test of the new lifecycle package with CGO-enabled Go tooling if available; record any unavailable check honestly.
- [ ] Request whole-branch code/security review, especially account isolation, state ownership, encrypted Store fencing, headers and shutdown. Fix evidenced findings and rerun affected checks.
- [ ] Document supported connectors and their login modes, remote callback instructions, local disconnect semantics, vault backup requirement, internal loopback lifecycle, entitlement limitations and lack of anti-ban guarantee. Explain no separate CLIProxyAPI deployment is required. Document upstream MIT attribution for any copied code/assets. Only mark spec Implemented once all required acceptance paths work; live OAuth consent remains user-driven and cannot be fabricated.
- [ ] Remove scratch probe files and test-only runtime artifacts after recording evidence. Keep existing provider UI actions and migrate every affected caller; no compatibility aliases or dead paths. Commit documentation with its feature changes; squash per-feature task commits before main integration. Do not merge, push, bump version or restart the user's real service without their instruction.

## Execution handoff

The user approved this plan and selected native implementation. Implementation is committed on `feat/provider-icons`; the application tree has passed server-tag Go tests/build, 71 JS tests and the OAuth package race detector. Independent security/gateway review findings were addressed. Real-account consent remains unverified and requires the account holder; browser authorization URL start/cancel was exercised for Claude and Codex.

## Execution notes

- The engine/management/transport bridge was introduced together to keep the integration compiling; focused lifecycle and gateway regressions followed.
- Use per-account private model prefixes with ForceModelPrefix, not request-context pinning: the SDK HTTP handler reconstructs execution context and drops arbitrary account pins. A two-account test reproduced incorrect routing before the namespace fix and passed afterward.
- Login creation grants carry an incarnation; ordinary refresh does not consume that grant or increment lifecycle version. Old incarnations cannot overwrite reconnect credentials even with larger SDK counters.
- SDK callback/authentication log entries are sanitized. Cancellation refuses to claim success once authorization is being committed; public account state projects terminal automatic-refresh failures as needs_login.
- Supported UI connectors are Claude and OpenAI/Codex browser OAuth. Additional connectors/device modes remain unadvertised under the spec's verification gate.
- No production service restart, main merge, or push was performed. Scratch services and browser tabs were stopped after smoke checks.
