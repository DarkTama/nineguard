# 0008. Traffic records the resolved Model ID alongside the Requested Model

- Status: Accepted
- Date: 2026-10-12

## Context

`traffic_logs.model` stores the Requested Model: the raw string the client sent. Clients may omit the Provider prefix (`gpt-4o` routes to the default provider), so one Model can appear under several strings. Model Quotas (ADR 0007) keyed on the raw string could be bypassed by changing how the model is written.

Options considered:

1. Match quotas on the raw `model` string.
2. Resolve to the canonical prefixed Model ID before the quota check and store it in a new `traffic_logs.model_id` column.
3. Same as 2 but overwrite `traffic_logs.model` with the canonical ID.

## Decision

Option 2. The proxy resolves every request to its Model ID before the access/disabled/quota checks and records it in `traffic_logs.model_id`. Model Quota usage sums by `(api_key_id, model_id, timestamp)` with a matching index. `traffic_logs.model` keeps the Requested Model unchanged.

Old rows are not backfilled: the default provider may have changed since they were written, so resolving them now could be wrong.

## Consequences

- Rewriting the model name no longer escapes a Model Quota.
- Existing reports keep showing what clients actually sent.
- On launch, Model Quota usage starts from zero for the current window.
- Usage history in the quota editor (1.5x/2x suggestions) uses best-effort matching on the raw `model` column and is labelled approximate; enforcement uses `model_id` only.
