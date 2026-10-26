# 0015. Embedded OAuth Provider Engine

- Status: Accepted
- Date: 2026-10-26
- Amends: [0011](0011-proxy-requests-stream-usage.md), [0014](0014-native-anthropic-messages-and-dual-header-auth.md) for explicitly SDK-backed providers only

## Context

Custom HTTP upstreams do not manage provider OAuth grants or refresh tokens. Implementing every provider's authentication and inference behavior in NineGuard would duplicate a substantial integration surface. Running an external engine would add another deployment and management boundary.

## Decision

Embed CLIProxyAPI SDK v8.0.43 in the NineGuard process. Existing providers migrate to the `http` backend; `cliproxy` providers identify their family explicitly. Prefixes and icons are independent routing/display metadata, not authentication hints.

NineGuard remains responsible for client authentication, model access, quota admission, plugins and traffic metering. The SDK is invoked only after admission and owns its provider-specific request translation. HTTP providers keep their existing native pass-through behavior.

The engine binds an authenticated IPv4 loopback listener and uses a custom encrypted SQLite credential store. Only explicit, administrator-protected NineGuard account/login operations are exposed. SDK management routes, raw Auth objects and token export are not exposed publicly. Account ids remain stable across reconnect; disconnect tombstones prevent late refresh from restoring credentials.

## Consequences

- Claude and OpenAI/Codex browser OAuth can be managed inside the Providers page without a separate engine installation.
- The SDK brings additional dependencies and process-global registries; one engine is allowed per process and upgrades are pinned/reviewed.
- Remote use needs manual callback URL submission when the provider redirects to the user's loopback address.
- Account entitlement, provider restrictions and revocation still apply; no anti-ban guarantee is made.
- Real consent remains user-driven; local fake-provider smoke tests do not prove a real account is authorized.
