# Future Planned Features

Planned enhancements discussed for post-plugin roadmap.

---

## 1. Image Detection & Multimodal Logging

### Goal
Track and flag requests containing images (multimodal prompts) across agents and tools.

### Mechanism
- **Inspection**: Parse incoming chat completion body in `proxy.go`. Check `messages[].content` array for items where `type == "image_url"`.
- **Database Schema**:
  - `ALTER TABLE traffic_logs ADD COLUMN has_images INTEGER DEFAULT 0;`
  - `ALTER TABLE traffic_logs ADD COLUMN image_count INTEGER DEFAULT 0;`
- **Telemetry & UI**:
  - Traffic Explorer row badge: 🖼️ icon showing image count (e.g. `🖼️ 2 img`).
  - Traffic filter: `has_images=1` (filter multimodal requests only).
  - Detail modal: displays attached image count and formats.

---

## 2. Heavy Token Usage Flagging (Spike Alerts)

### Goal
Highlight individual requests that consume massive context or spike unexpectedly.

### Mechanism
- **Configuration**: System setting `heavy_token_threshold` in `settings` table (default: e.g. `8000` tokens, user-adjustable in UI).
- **Evaluation**: On request completion in `proxy.go`, check `total_tokens >= heavy_token_threshold`.
- **Telemetry & UI**:
  - Traffic Explorer: yellow/orange warning badge ⚠️ `Heavy (<N>k tok)`. Filter option `heavy_only=1`.
  - System Logs: emits `WARN` log entry (`source=traffic`, e.g. `token_spike: key "pi-dev" consumed 18,400 tokens`).
- **Future Extension**: Optional pre-flight rejection threshold (blocking requests whose prompt exceeds hard safety cap before forwarding to provider).

---

## 3. Token Quotas & Limiting per API Key

### Goal
Enforce daily, weekly, monthly, or lifetime token budgets per client API key without deleting or invalidating the credential.

### Mechanism
- **Database Schema**:
  - `ALTER TABLE api_keys ADD COLUMN quota_limit INTEGER DEFAULT 0;` (0 = unlimited)
  - `ALTER TABLE api_keys ADD COLUMN quota_period TEXT DEFAULT 'none';` (`none`, `daily`, `weekly`, `monthly`, `total`)
- **Enforcement**:
  - In `proxy.go`, before forwarding, compute tokens consumed in current window via SQLite index `idx_traffic_key_id_ts(api_key_id, timestamp)`:
    - `daily`: `timestamp >= datetime('now', 'start of day')`
    - `weekly`: `timestamp >= datetime('now', 'weekday 0', '-6 days', 'start of day')`
    - `monthly`: `timestamp >= datetime('now', 'start of month')`
    - `total`: all-time sum
  - If limit reached: return **HTTP 429 Too Many Requests**:
    ```json
    {
      "error": {
        "message": "API key token quota exceeded (120,000 / 100,000 tokens daily). Resets at midnight UTC.",
        "type": "insufficient_quota",
        "code": "quota_exceeded"
      }
    }
    ```
  - Automatically unblocks when next period begins or administrator raises quota limit. Key is never deleted.
- **UI**:
  - Key modal: inputs for `Quota Limit` and `Period`.
  - Endpoints table: quota consumption progress bar (e.g. `340k / 500k (68%)`). Red highlight when quota exhausted.

---

## 4. Token Burn Rate & Velocity Baselines

### Goal
Provide historical burn-rate context in the UI so administrators know what quota limits to set instead of guessing.

### Mechanism
- **Key Modal Helper**:
  - When editing a key, calculate and show past usage velocity pills next to the quota input:
    - `Past 24h: 38k tokens`
    - `7d avg: 45k tokens/day`
    - `30d total: 1.2M tokens`
  - Quick-preset buttons: `Set 1.5x daily avg (68k)` or `Set 2x daily avg (90k)`.
- **Endpoints & Keys Table**:
  - Display burn velocity below total tokens: e.g. `45.1M total (~120k/day)`.
- **Usage Reports View**:
  - System-wide burn rate metric: e.g. `~450k tokens/day across all keys`.
