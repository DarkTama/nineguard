# Embedded Multi-Provider OAuth and Provider Identity — Design

- Date: 2026-10-26
- Status: Implemented — Claude and Codex browser OAuth; real-account consent remains user-driven
- Reference revision: CLIProxyAPI `307803510bce87f986fcf7b1a84190f105c7b4aa` (MIT)
- Selected approach: embed CLIProxyAPI SDK in NineGuard's process, not a separately installed service.

## Goal

NineGuard users can connect provider accounts through OAuth, manage connection and token lifecycle from Providers, and send requests through NineGuard's existing client-key permissions, quotas, plugin pipeline, traffic records and pricing. Custom OpenAI-compatible upstream URLs remain supported. Provider identities have bundled icons or a custom image URL.

The requested first-class OAuth accounts are Claude and OpenAI/Codex. This is not an API-key-only substitute. Additional provider login flows are exposed only when the pinned SDK actually supports them end to end.

No anti-ban guarantee, account-limit evasion, or identity-spoofing feature is part of this design. Provider terms and subscription entitlements remain applicable. The route prefix is a user-selected label and is not evidence of its authentication method.

## Reference findings

Sources are pinned rather than following the moving default branch:

- `sdk/cliproxy/builder.go`: public service builder, custom auth managers and watcher injection.
- `sdk/cliproxy/service_lifecycle.go`: service initializes executors, loads credentials, starts refresh and an HTTP listener.
- `sdk/api/management.go`: exported management handler and OAuth callback/session helpers.
- `sdk/auth/claude.go`: PKCE browser login, state verification and callback handling.
- `sdk/auth/codex.go`, `sdk/auth/codex_device.go`: Codex browser/device login.
- `sdk/cliproxy/auth/store.go`: replaceable `List`, `Save`, `Delete` persistence contract.
- `sdk/cliproxy/auth/types.go`: credentials may appear in `Metadata`; this object MUST NOT be returned directly to NineGuard clients.
- `sdk/cliproxy/executor/types.go`: selected/pinned account execution metadata.
- `sdk/cliproxy/service_executors.go`: registration of Claude, Codex, Antigravity, Kimi, xAI, Devin and Meta executors.

The repository module is v8; its SDK documentation still contains v6 examples. Integration must compile against the pinned v8 public interfaces. No imports of upstream `internal/*` packages.

## Architecture

### Responsibility boundary

NineGuard owns the external HTTP listener, dashboard sessions, provider configuration, client keys, prefix resolution, firewall, quotas, plugin decisions, traffic and pricing.

CLIProxyAPI owns OAuth provider-specific token exchange, refresh behavior, account-backed execution and protocol translation for SDK-backed providers.

Existing custom upstream providers keep their current HTTP path; they do not route through the SDK merely because the SDK is installed.

The SDK runs in the same process behind an internal IPv4 loopback listener. Its address and an ephemeral random engine credential are generated/managed by NineGuard and never exposed in provider API responses. No caller-supplied URL can select that internal listener. No separate Docker container or external engine installation is required. SDK management routes are not published as a reverse-proxy wildcard.

### Provider and account identity

A Provider remains a stable routing target with id, name, prefix, default/enabled state and icon. Add an explicit backend discriminator for custom HTTP versus SDK-backed execution, and a provider-family discriminator for SDK-backed entries. Existing rows migrate to custom HTTP without changing ids or prefixes.

An OAuth account belongs to a Provider. Account ids survive refresh and reconnect. Several accounts can be attached to one Provider and are visible individually. An explicit account selection must remain scoped to the selected Provider, never another Provider of the same family.

Disconnect removes the account's local credentials and stops new admissions through that account; it is not a promise to revoke the upstream grant unless a verified revocation endpoint is supported. Removing a Provider cancels its pending login sessions and removes its account bindings. Requests already in progress follow the gateway's existing cancellation behavior.

### Supported provider presentation

- Custom URL: existing OpenAI-compatible gateways, including 9router, OmniRoute and other custom endpoints, with existing API-key behavior.
- Claude: OAuth login via the SDK's Claude connector.
- OpenAI / Codex: OAuth login explicitly labelled Codex. It does not create an OpenAI Platform API key or promise access to every OpenAI API product.
- Antigravity, Kimi, xAI, Meta and Devin: expose only login and execution capabilities verified in the pinned SDK and proven by integration probes before shipping.
- Do not label native Gemini CLI OAuth as supported merely because Antigravity or Gemini API-key execution exists. Discovery found no standalone Gemini authenticator in the inspected `sdk/auth` inventory.

Every option displayed as supported must have working login, persistence, account registration, model publication and execution. Unsupported flows must not appear as enabled buttons or placeholders. If a connector cannot meet this contract through public SDK APIs, report that fact for a scope decision before claiming delivery.

## OAuth lifecycle

Management operations require NineGuard administrator authorization, not a client key or an operator session. Existing unrelated provider permissions are intentionally unchanged.

Starting login creates an expiring, random session bound to administrator, target Provider and connector. PKCE verifier and token exchange stay server-side. Callback completion validates state, ownership, connector, expiry and one-time consumption. Never permit open redirects from callback parameters.

The dashboard opens the authorization URL in the user's browser, not the server's browser. For loopback callback flows, document local use and provide the connector's supported manual callback submission for a remote server. A device-code flow is exposed only for connectors that actually support it. Device-code polling follows the upstream interval and expiry, and stops on cancellation or server shutdown.

Login states: pending, connected, cancelled, expired, failed. Account states: connected, refreshing, needs_login, disabled. Transient network refresh failures preserve credentials; invalid/revoked refresh grants transition to needs_login. The UI shows a sanitized error and reconnect action rather than a token blob or upstream response dump.

Login completion must register the account and its model catalog in the running SDK immediately. It cannot depend on plaintext auth-file watching. The integration probe must verify custom-store persistence plus runtime publication; use public post-auth hooks/SDK update APIs where necessary.

## Credential persistence

Implement the SDK's credential Store on NineGuard SQLite with AES-256-GCM via the existing vault. Persist provider/account identity and safe status metadata separately from the encrypted credential payload. Handle encryption/decryption errors explicitly; do not persist an empty substitute credential.

No SDK default plaintext token store, token export endpoint, or direct serialization of upstream `Auth` objects. Token material includes access/refresh/id tokens and provider-specific secret metadata. Transient authorization codes and verifiers are secrets too; do not log them or put them in traffic payload capture.

Credential updates are versioned and atomic. Concurrent refresh must not overwrite newer tokens, and late login/refresh completion cannot recreate an account after disconnect. Rotation must survive process restart. Follow the SDK Store's ownership/version invariants and copy mutable metadata as required.

SQLite has one connection: close rows before additional queries, use only tx within transactions, and never make network requests while holding a database transaction or manager lock.

## Execution and protocol behavior

SDK execution occurs after NineGuard client-key authentication, canonical model resolution, access/disabled checks and quota admission. Prefix removal remains consistent with the current provider contract.

NineGuard plugins keep receiving the client's protocol; SDK-backed protocol conversion occurs at the engine boundary after those decisions. This requires an explicit amendment to the existing no-core-translation architecture: no ad hoc conversion is added to the proxy core. Existing HTTP providers retain native pass-through.

Support buffered and SSE paths with disconnect cancellation, correct error status/envelopes, tool calls and usage metering. Never count one external request twice because it crossed the private engine. Do not silently switch to an unrelated Provider/account when the selected Provider cannot serve a model.

Model sync uses the connected Provider's SDK catalog, not a global catalog containing other Providers' models. Losing credentials must not fabricate model availability or delete historical traffic identities.

## Providers UI direction

The page's job is to answer: which upstream is connected, through which account or URL, and what action fixes an unhealthy connection.

Reuse NineGuard's existing palette: light background `#f4f3f1`, panel `#ffffff`, text `#1c1917`, border `#e7e4e0`, accent `#c2410c`, success `#15803d`; existing dark theme variables remain supported. Keep the bundled NineGuard Mono family for consistency and readable model ids, not a new ornamental display typeface.

Replace the large routing tutorial with a short collapsible explanation. Providers appear as compact identity rows/sections: icon and name, prefix, connection method, account/endpoint, supported protocol, status and relevant actions. Account detail expands under its Provider. Primary actions are Add provider and Connect account; deletion remains separately labelled and confirmed.

The add/edit form separates provider choice, authentication, routing and optional advanced settings. Selecting OAuth shows login/account controls rather than a misleading API-key field. Custom URL shows route/key fields. Existing quota tracker and upstream token-saving options remain available where applicable.

Icon selection: auto, bundled preset, or custom HTTP(S) image URL. Broken images fall back to the provider's initial/generic mark without retry loops. Custom images load as images, never injected SVG/HTML, with no referrer and no server-side URL fetch. Mention that a remote image host sees browser requests. A brand preset is identity metadata, not an authentication type.

Mobile uses a single column, wrapping identity metadata and explicit action labels; long URLs and model ids do not widen the page. Keyboard focus, labels, status semantics and reduced motion are required. No decorative gradients, status dots or repeated stat cards.

## Acceptance and verification

- Existing databases/providers keep routing with their original ids, prefixes, defaults and keys.
- Claude and Codex OAuth login, cancellation, failed callback and expired session work through NineGuard admin UI.
- Cross-user, wrong-provider, replayed or expired callbacks cannot attach credentials.
- Token storage is encrypted; admin/client JSON and logs do not contain secret fields.
- Successful login and refresh publish usable accounts without a restart or plaintext token file.
- Concurrent refresh, disconnect during login/refresh, revocation and process restart preserve the stated lifecycle.
- Connected-provider-only model listing and buffered/SSE requests pass through the real NineGuard admission/traffic path.
- Custom HTTP routing remains unchanged; protocol/account isolation and quota metering are regression-tested.
- Provider icons persist through create/edit/restart, invalid schemes are rejected, and broken images fall back.
- Browser smoke: 390px and desktop, both themes, keyboard login controls and status/error states.
- Run the Go server-tag test/build gates and existing JS tests. Real provider login requires account-holder interaction; do not claim real-account verification from mock OAuth tests.

### Implementation evidence and limits

- SDK v8.0.43 runs in-process with encrypted SQLite credentials and no token JSON files. Restored models are published through the SDK's public post-persist synchronization route.
- Account-specific private model namespaces enforce selection, because the SDK HTTP handlers do not preserve arbitrary request-context account pins. These namespaces are stripped from the public catalog and upstream model id.
- Claude and Codex authorization URL creation/cancellation were exercised through the real Providers UI. Real-account consent/token exchange was not performed by the assistant.
- Actual SDK buffered and SSE inference was exercised against a local fake upstream; full NineGuard admission denied forbidden/disabled models and recorded 14 tokens exactly once.
- Store regressions cover encrypted restart, Storage payload serialization, generation reset, disconnect/reconnect incarnation fencing and refresh during pending reconnect. Runtime status reflects terminal invalid-grant/401 errors without exposing upstream bodies.
- Browser checks cover desktop and 390px mobile, light/dark, custom URL, presets and broken-image fallback. Other connectors and device-code login are not advertised until verified; the current connector catalog exposes Claude/Codex browser login.

## Review decision

The user selected embedded CLIProxyAPI SDK and approved this design. The implementation plan starts with a scratch compile/runtime probe of public SDK embedding, custom encrypted Store and OAuth runtime publication; an unreachable public hook is an integration blocker, not permission to silently substitute an external service or API keys.
