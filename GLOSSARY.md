# NineGuard Glossary

## Access

**API Key** (Client Key)
A NineGuard-issued credential (`sk-ng-...`) identifying one agent, user, or tool. Never the upstream provider's key.

**Model Group**
A named set of model IDs or patterns. Serves two independent roles:
1. *Access template* — an API Key in group mode may only use models in its linked groups.
2. *Plugin scope* — plugins bound to a group apply to any request whose model belongs to that group, regardless of which API Key sent it.

**Provider**
An upstream OpenAI-compatible endpoint, addressed through its routing prefix (e.g. `9router/...`).

## Plugins

**Plugin**
A request transformer that modifies a chat completion request after access checks pass and before it is forwarded upstream. Plugins never modify responses.

**Built-in Plugin**
A plugin shipped inside NineGuard (e.g. Caveman, Ponytail, Headroom connector).

**HTTP Plugin**
A third-party plugin running as a separate service that NineGuard calls over HTTP. "Installing" a third-party plugin means registering its endpoint.

**Plugin Binding**
The attachment of a plugin to a Scope, with a state (`on`, `off`, or `inherit`) and optional settings overrides.

**Scope**
Where a Plugin Binding applies: Global (every request), Model Group (requests for a model in that group), or API Key (requests from that key).

**Precedence**
How conflicting Plugin Bindings resolve: API Key overrides Model Group, which overrides Global. `inherit` defers to the next broader scope.

**Group Priority**
A number on each Model Group that decides which group's Plugin Binding wins when a model belongs to several groups with conflicting bindings. Higher wins.

**Plugin Pipeline**
The single, globally ordered sequence in which applicable plugins run on a request. Order is set once, not per scope.

**Bypassable Plugin**
A plugin a client may switch off for one request. Plugins that protect (fail-closed) are never bypassable.

**Rejection**
A plugin's decision to block a request outright instead of transforming it.

## Reporting

**Tokens Saved**
Input tokens a plugin removed from a request, as reported by the plugin itself. Never estimated.

**Plugin Overhead**
Input tokens a plugin added to a request (e.g. an injected prompt).

**Failure Policy**
What happens when a plugin errors or times out. *Fail-open*: forward the request unmodified. *Fail-closed*: reject the request.
