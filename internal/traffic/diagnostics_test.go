package traffic_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/traffic"
)

func TestDiagnosticsTrafficLogAndQuery(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	// Record an entry with usage diagnostics
	entry := &traffic.LogEntry{
		APIKey:            "sk-ng-diag-key",
		APIKeyName:        "diag-agent",
		Model:             "claude-opus-4.5",
		ModelID:           "9router/claude-opus-4.5",
		StatusCode:        200,
		ClientIP:          "127.0.0.1",
		PromptTokens:      1000,
		CompletionTokens:  500,
		TotalTokens:       1500,
		ApiFormat:         "chat",
		CacheReadTokens:   800,
		CacheWriteTokens:  100,
		ReasoningTokens:   250,
		ThinkingRequested: true,
		ThinkingDelivered: true,
		ServedModel:       "claude-opus-4-5-20251101",
		ModelFallback:     false,
		UsageEstimated:    false,
	}

	if err := mgr.Record(entry); err != nil {
		t.Fatalf("Record entry: %v", err)
	}

	// Query logs
	logs, total, err := mgr.QueryLogs(traffic.FilterParams{Limit: 10})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if total != 1 || len(logs) != 1 {
		t.Fatalf("expected 1 log, got total=%d len=%d", total, len(logs))
	}

	got := logs[0]
	if got.ApiFormat != "chat" {
		t.Errorf("ApiFormat = %q; expected 'chat'", got.ApiFormat)
	}
	if got.CacheReadTokens != 800 {
		t.Errorf("CacheReadTokens = %d; expected 800", got.CacheReadTokens)
	}
	if got.CacheWriteTokens != 100 {
		t.Errorf("CacheWriteTokens = %d; expected 100", got.CacheWriteTokens)
	}
	if got.ReasoningTokens != 250 {
		t.Errorf("ReasoningTokens = %d; expected 250", got.ReasoningTokens)
	}
	if !got.ThinkingRequested {
		t.Errorf("ThinkingRequested = false; expected true")
	}
	if !got.ThinkingDelivered {
		t.Errorf("ThinkingDelivered = false; expected true")
	}
	if got.ServedModel != "claude-opus-4-5-20251101" {
		t.Errorf("ServedModel = %q; expected 'claude-opus-4-5-20251101'", got.ServedModel)
	}
	if got.ModelFallback {
		t.Errorf("ModelFallback = true; expected false")
	}
	if got.UsageEstimated {
		t.Errorf("UsageEstimated = true; expected false")
	}
}

func TestDiagnosticsQueryFilters(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer d.Close()

	mgr := traffic.NewManager(d)

	// Entry 1: chat, thinking delivered
	_ = mgr.Record(&traffic.LogEntry{
		APIKey: "sk-1", Model: "m1", StatusCode: 200,
		ApiFormat: "chat", ThinkingRequested: true, ThinkingDelivered: true,
	})
	// Entry 2: chat, thinking lost, fallback
	_ = mgr.Record(&traffic.LogEntry{
		APIKey: "sk-2", Model: "m2", StatusCode: 200,
		ApiFormat: "chat", ThinkingRequested: true, ThinkingDelivered: false, ModelFallback: true,
	})
	// Entry 3: messages format
	_ = mgr.Record(&traffic.LogEntry{
		APIKey: "sk-3", Model: "m3", StatusCode: 200,
		ApiFormat: "messages",
	})
	// Entry 4: estimated usage
	_ = mgr.Record(&traffic.LogEntry{
		APIKey: "sk-4", Model: "m4", StatusCode: 200,
		ApiFormat: "chat", UsageEstimated: true,
	})

	tTrue := true

	// Filter by ApiFormat
	logs, total, _ := mgr.QueryLogs(traffic.FilterParams{ApiFormat: "messages"})
	if total != 1 || len(logs) != 1 || logs[0].Model != "m3" {
		t.Errorf("ApiFormat filter failed: total=%d", total)
	}

	// Filter by Thinking = lost
	logs, total, _ = mgr.QueryLogs(traffic.FilterParams{Thinking: "lost"})
	if total != 1 || len(logs) != 1 || logs[0].Model != "m2" {
		t.Errorf("Thinking=lost filter failed: total=%d", total)
	}

	// Filter by Thinking = delivered
	logs, total, _ = mgr.QueryLogs(traffic.FilterParams{Thinking: "delivered"})
	if total != 1 || len(logs) != 1 || logs[0].Model != "m1" {
		t.Errorf("Thinking=delivered filter failed: total=%d", total)
	}

	// Filter by Thinking = requested
	logs, total, _ = mgr.QueryLogs(traffic.FilterParams{Thinking: "requested"})
	if total != 2 || len(logs) != 2 {
		t.Errorf("Thinking=requested filter failed: total=%d", total)
	}

	// Filter by Fallback = true
	logs, total, _ = mgr.QueryLogs(traffic.FilterParams{Fallback: &tTrue})
	if total != 1 || len(logs) != 1 || logs[0].Model != "m2" {
		t.Errorf("Fallback filter failed: total=%d", total)
	}

	// Filter by Estimated = true
	logs, total, _ = mgr.QueryLogs(traffic.FilterParams{Estimated: &tTrue})
	if total != 1 || len(logs) != 1 || logs[0].Model != "m4" {
		t.Errorf("Estimated filter failed: total=%d", total)
	}
}
