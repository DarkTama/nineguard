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

**Model**
A model ID NineGuard knows about, prefixed by its Provider (e.g. `9router/gpt-4o`). The ID is unique across Providers.

**Disabled Model**
A Model an admin switched off. An admin choice, independent of what the Provider offers.

**Removed Model**
A Model its Provider stopped listing. Detected by sync, never set by an admin. Its record is kept, so old traffic can still be tagged "removed". Clears itself when the Provider lists the Model again. Independent of Disabled Model.

**Unavailable Entry**
A Model Group member that names a Removed Model. A count label in the group view only. Wildcard members are never Unavailable Entries.

## Plugins

**Plugin**
A request transformer that modifies a chat completion request after access checks pass and before it is forwarded upstream. Plugins never modify responses.

**Built-in Plugin**
A plugin shipped inside NineGuard (e.g. Caveman, Ponytail, Headroom connector).

**HTTP Plugin**
A third-party plugin running as a separate service that NineGuard calls over HTTP. "Installing" a third-party plugin means registering its endpoint.

**Plugin Binding**
The attachment of a plugin to a Scope, with a state (`on`, `off`, or `inherit`) and optional settings overrides.

**Scope Override**
An explicit Plugin Binding on a Model Group or API Key setting state to `on` or `off`, overriding the broader scope. When set to `inherit`, the override is removed and the broader scope decides.

**Scope**
Where a Plugin Binding applies: Global (every request), Model Group (requests for a model in that group), or API Key (requests from that key). The UI always labels Global as "All keys, all models".

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

**Token Saver**
A plugin whose purpose is reducing tokens: any plugin in the `input_compression` or `output_style` category.

**Plugin Category**
Which side of the exchange a plugin reduces. *Input compression* shrinks what is sent to the model (Headroom). *Output style* instructs the model to answer more briefly (Caveman, Ponytail). *Other* covers everything else.

**Overlap Warning**
An advisory notice that token savers stack for some API Key and model: two output-style plugins together, or a token saver on a provider marked as doing its own token saving. Never blocks a request.

**Upstream Token Saving**
A flag on a Provider declaring that the provider already applies token saving itself (e.g. 9router RTK, another NineGuard). Declared by the user because NineGuard cannot detect it.

**Overcompression**
Loss of answer quality caused by stacking token savers: needed context removed, or style rules piled up until answers become too terse.

## Reporting

**Last Active**
The time of the most recent request authenticated with an API Key (or sent to a model), regardless of outcome — blocked and failed requests count. Never limited to a report period.

**Period**
The time window a report or filter covers, with day boundaries in the viewer's own timezone.

**Tokens Saved**
Input tokens a plugin removed from a request, as reported by the plugin itself. Never estimated.

**Plugin Overhead**
Input tokens a plugin added to a request (e.g. an injected prompt).

**Failure Policy**
What happens when a plugin errors or times out. *Fail-open*: forward the request unmodified. *Fail-closed*: reject the request.

## Quotas & Traffic Controls

**Key Quota** (formerly Token Quota)
The maximum cumulative tokens (`total_tokens`) an API Key may consume across all models within its Quota Period before subsequent requests are blocked.

**Model Quota**
The maximum cumulative tokens (always greater than zero) an API Key may consume on one exact Model within the quota's own Quota Period. Usage on other Models never counts against it. A Model with no Model Quota on a key in model mode is unlimited for that key. Model Quotas belong to an API Key only; Model Groups never carry or supply quota limits.

**Requested Model**
The model name exactly as a client sent it, which may omit the Provider prefix (e.g. `gpt-4o`). Resolves to exactly one Model. Model Quotas always count against the resolved Model, never the Requested Model.

**Inactive Model Quota**
A Model Quota whose Model the API Key can currently no longer use (access lost, Disabled Model, or Removed Model). Kept with its limit, enforces nothing, and applies again if access returns. A Model Quota can never be created for a Model the key cannot use.

**Quota Mode**
Which kind of quota an API Key enforces: `key` (a single Key Quota) or `model` (Model Quotas). Exactly one mode is enforced at a time. Settings of the inactive mode are kept, not deleted, and take effect again if the mode is switched back.

**Quota Period**
The fixed time cycle for resetting a Key Quota or Model Quota: `daily`, `weekly`, `monthly`, or `total` (all-time). Unlike reporting periods, Quota Periods always anchor to UTC midnight (ISO Monday 00:00 UTC for weekly; 1st of month 00:00 UTC for monthly).

**Soft Post-Facto Enforcement**
The mechanism where pre-flight checks block requests only when accumulated tokens already meet or exceed the quota. An admitted request is never truncated mid-flight; final token count is tallied on completion.

**Near-Limit Quota**
A Key Quota or Model Quota whose usage in the current Quota Period has reached the system-wide warning percentage (default 80%), including quotas already exhausted. Advisory only; never blocks a request.

**Heavy Request**
A request whose total tokens meet or exceed the system-wide `heavy_token_threshold`, triggering an advisory warning badge and system log alert without blocking traffic.

