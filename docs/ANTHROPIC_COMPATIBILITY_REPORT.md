# Anthropic Protocol & 9router Compatibility Report

Comprehensive findings, root causes, and resolutions for Anthropic SDK `/v1/messages` integration, cascaded gateway topologies, and upstream translation quirks (v1.5.1 – v1.5.3).

---

## 1. Environment & Request Flow Topology

```
[Client: Coding Agent / Claude SDK / CLI]
       │
       ▼ (Auth: x-api-key / Bearer)
[NineGuard Edge Gateway] (v1.5.1 - v1.5.3)
       │
       ▼ (Forward: path passthrough, header sanitation)
[NineGuard Upstream Relay]
       │
       ▼ (Reverse Proxy)
[Upstream 9router] (v0.5.104)
       │
       ▼ (OAuth / API Provider Route: cc/)
[Anthropic Upstream API]
```

---

## 2. Issue 1: Tool Name Suffix Mutation (`_ide`, `_ide_ide`) on `/v1/messages`

### Symptom
- Client provides tools: `read`, `bash`, `edit`, `write`, `codemode`.
- Assistant tool calls returned to client: `bash_ide` (24x), `bash_ide_ide` (1x), `codemode_ide` (1x).
- Client agent rejects execution: `Tool bash_ide not found`.
- Assistant enters a retry loop attempting alternative tool names until the session is aborted.

### Root Cause Analysis
1. **Upstream 9router Cloaking:**
   - Confirmed in 9router v0.5.104 `open-sse/utils/claudeCloaking.js` (`CLAUDE_TOOL_SUFFIX = "_ide"`).
   - On `cc/` (Claude OAuth) upstream routes, 9router renames offered tools: `bash` → `bash_ide`.
   - Claude upstream model selects and emits `bash_ide`.
   - 9router response stream parser fails to decloak the tool name back to `bash` due to SSE `toolNameMap` cache misses.
2. **Double-Cloaking Loop:**
   - Subsequent conversational turns carry prior conversation history containing `bash_ide`.
   - 9router cloaking re-processes the history and appends another suffix: `bash_ide` → `bash_ide_ide`.
3. **Upstream Issue Report:**
   - Documented and reported upstream at [https://github.com/decolua/9router/issues/5170](https://github.com/decolua/9router/issues/5170).

### NineGuard Resolution (v1.5.1+)
NineGuard implements comprehensive mitigation in `internal/proxy/decloak.go`:
- **Request History De-poisoning:** Scans incoming conversation context and strips cloaked suffixes (`_ide`, `_cc`) from historical tool calls (`tool_use.name`, `function.name`) and tool result IDs before forwarding upstream.
- **SSE & Buffered JSON Auto-Decloaking:** Intercepts response streams and buffered JSON bodies, verifies emitted tool names against the request's offered tools schema, and strips suffixes to restore original tool names.
- **Decloak Telemetry Tag (v1.5.3):** Tracks and logs decloak events in traffic records (`decloaked = 1`) for end-to-end auditability in Traffic Explorer.

---

## 3. Issue 2: Thinking Lost (`🧠✗`) & Multi-Turn Thinking

### Symptom
- Client targets `claude-opus-5-5` or `claude-3-7-sonnet` with thinking requested.
- Requests complete with HTTP `200 OK`, but Traffic Explorer flags requests with `🧠✗ Thinking was requested but the upstream response shows no reasoning`.

### Verified Root Causes & Findings
Investigation across v1.5.0 through v1.5.2 verified three distinct causes:

1. **Endpoint Mislabelling in Gateway UI (v1.5.0):**
   - In the initial investigation, Request #22123 was diagnosed as a chat failure, but it was actually a native Anthropic `POST /v1/messages` request.
   - Traffic logging in v1.5.0 had a hardcoded endpoint label (`POST /v1/chat/completions`), concealing the actual protocol format used by the client.
   - *Fixed in v1.5.1:* Dynamic endpoint format detection assigns accurate `api_format` (`messages`, `chat`, `responses`).

2. **9router Thinking Deletion on Multi-Turn Tool Results:**
   - On OpenAI `/v1/chat/completions` requests, 9router's `normalizeThinkingConfig()` deleted `body.thinking` whenever the prompt history included a tool-result turn (`role: 'tool'`).
   - Consequently, subsequent turns in tool-calling workflows lost reasoning parameters.
   - Reported upstream at [https://github.com/decolua/9router/issues/5171](https://github.com/decolua/9router/issues/5171).
   - *Fixed in NineGuard v1.5.1:* `PreserveThinkingIntent()` injects and restores client thinking intent across tool turns before forwarding to upstream.

3. **Claude Opus 5.5 Adaptive Thinking & Signature-Only Deltas:**
   - Claude Opus 5.5 upstream employs adaptive thinking, frequently returning thinking content blocks with thinking signature deltas but 0 characters of human-visible reasoning text.
   - When converted to the OpenAI chat completions format, no reasoning delta (`reasoning_content`) or reasoning token count is exposed.
   - *Fixed in NineGuard v1.5.2+:* When Claude models are accessed via OpenAI chat completions without observable reasoning text, NineGuard labels the status as **Unverifiable** (`🧠?`) rather than a false-positive **Lost** (`🧠✗`).

---

## 4. Issue 3: Hop-by-Hop `Connection: close` Alternating 200/400 on SSE Streams

### Symptom
- Sequential Server-Sent Events (SSE) streaming requests exhibited alternating success and failure (`200 OK` followed by `400 Bad Request` or unexpected stream drops).

### Root Cause Analysis
- Client agents and intermediate proxies often supply hop-by-hop headers such as `Connection: close`.
- When NineGuard forwarded client hop-by-hop headers verbatim to the upstream Node.js server, the upstream server closed the underlying transport immediately or reset stream socket state while processing persistent HTTP connections.
- Subsequent pipelined or reused connections resulted in abrupt termination or HTTP `400 Bad Request`.

### NineGuard Resolution (v1.5.2)
- Implemented `RemoveHopByHopHeaders()` in the reverse proxy transport conforming to **RFC 7230 Section 6.1**.
- Systematically strips hop-by-hop headers before upstream forwarding:
  - `Connection`
  - `Keep-Alive`
  - `Proxy-Authenticate`
  - `Proxy-Authorization`
  - `TE`
  - `Trailers`
  - `Transfer-Encoding`
  - `Upgrade`

---

## 5. Observability Enhancements (v1.5.1 – v1.5.3)

To ensure full transparency across multi-format, multi-turn AI traffic, the following enhancements have been delivered:

1. **Protocol Filter Chips & Badges (v1.5.3):**
   - Dedicated filter chips in Traffic Explorer toolbar:
     - `/v1/messages` (amber/orange)
     - `/v1/chat` (sky/teal)
     - `/v1/responses` (emerald)
   - Compact protocol badges on non-standard request rows (`messages`, `responses`).
   - One-click "Filter {protocol}" action in request detail drawers.

2. **Accurate 'Copy as cURL' Generation (v1.5.3):**
   - Automatically adapts the cURL path using `endpointPathForFormat(api_format)` (`/v1/messages`, `/v1/responses`, `/v1/chat/completions`).
   - Injects required Anthropic headers (`-H "anthropic-version: 2023-06-01"`) when generating cURL for `/v1/messages` requests.

3. **Reconstructed SSE Stream Summaries in Payload Inspector (v1.5.3):**
   - Live stream parsers reconstruct full completion text, reasoning blocks, and tool call invocations from incremental SSE chunks for post-request inspection.

4. **JSON-Safe 512KB Payload Clamping (v1.5.3):**
   - Large request/response bodies are capped at 512KB with structural JSON safety to prevent parsing errors and memory exhaustion in the dashboard.

5. **Decloak Telemetry Badge (v1.5.3):**
   - Visually flags requests where automatic tool-name decloaking or history de-poisoning intervened.
