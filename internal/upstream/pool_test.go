package upstream_test

import (
	"testing"
	"time"

	"nineguard/internal/upstream"
)

func TestMergeAccountPools_MathAndEarliestReset(t *testing.T) {
	now := time.Now()
	t1 := now.Add(22 * time.Hour)
	t2 := now.Add(23 * time.Hour)

	// Simulate user screenshot with 2 connections
	conns := []upstream.ConnectionQuota{
		{
			ConnectionID: "acc1",
			AccountLabel: "personal@gmail.com",
			Status:       "ok",
			Buckets: []upstream.QuotaBucket{
				{
					Name:                "Gemini (Weekly)",
					Used:                778,
					Total:               1000,
					Remaining:           222,
					RemainingPercentage: 22,
					ResetAt:             &t1,
				},
				{
					Name:                "Claude & GPT (5h)",
					Used:                0,
					Total:               1000,
					Remaining:           1000,
					RemainingPercentage: 100,
					ResetAt:             &t2,
				},
			},
		},
		{
			ConnectionID: "acc2",
			AccountLabel: "work@corp.com",
			Status:       "ok",
			Buckets: []upstream.QuotaBucket{
				{
					Name:                "Gemini (Weekly)",
					Used:                777,
					Total:               1000,
					Remaining:           223,
					RemainingPercentage: 22,
					ResetAt:             &t2,
				},
				{
					Name:                "Claude & GPT (5h)",
					Used:                0,
					Total:               1000,
					Remaining:           1000,
					RemainingPercentage: 100,
					ResetAt:             &t1,
				},
			},
		},
	}

	merged := upstream.MergeAccountPools("9router", conns)
	if merged == nil {
		t.Fatalf("expected non-nil merged pool")
	}

	if merged.Provider != "9router" {
		t.Fatalf("expected provider 9router, got %s", merged.Provider)
	}
	if !merged.Warning {
		t.Fatalf("expected merged pool to flag warning due to Gemini 22%%")
	}

	if len(merged.Buckets) != 2 {
		t.Fatalf("expected 2 merged buckets, got %d", len(merged.Buckets))
	}

	var geminiWeekly, claude5h *upstream.QuotaBucket
	for i := range merged.Buckets {
		if merged.Buckets[i].Name == "Gemini (Weekly)" {
			geminiWeekly = &merged.Buckets[i]
		} else if merged.Buckets[i].Name == "Claude & GPT (5h)" {
			claude5h = &merged.Buckets[i]
		}
	}

	if geminiWeekly == nil {
		t.Fatalf("missing Gemini (Weekly) bucket")
	}
	if geminiWeekly.Used != 1555 || geminiWeekly.Total != 2000 {
		t.Fatalf("expected 1555/2000, got %d/%d", geminiWeekly.Used, geminiWeekly.Total)
	}
	if geminiWeekly.Remaining != 445 {
		t.Fatalf("expected remaining 445, got %d", geminiWeekly.Remaining)
	}
	if geminiWeekly.RemainingPercentage != 22 {
		t.Fatalf("expected 22%%, got %d%%", geminiWeekly.RemainingPercentage)
	}
	if !geminiWeekly.Warning {
		t.Fatalf("expected warning on Gemini Weekly")
	}
	if geminiWeekly.ResetAt == nil || !geminiWeekly.ResetAt.Equal(t1) {
		t.Fatalf("expected earliest reset %v, got %v", t1, geminiWeekly.ResetAt)
	}

	if claude5h == nil {
		t.Fatalf("missing Claude & GPT (5h) bucket")
	}
	if claude5h.Used != 0 || claude5h.Total != 2000 {
		t.Fatalf("expected 0/2000, got %d/%d", claude5h.Used, claude5h.Total)
	}
	if claude5h.RemainingPercentage != 100 || claude5h.Warning {
		t.Fatalf("expected 100%% without warning, got %d%%", claude5h.RemainingPercentage)
	}
}

func TestFilterPoolsByModelAccess(t *testing.T) {
	pools := []upstream.MergedPoolQuota{
		{
			Provider:      "9router",
			Family:        "Claude",
			AllowedModels: []string{"claude-3-7-sonnet", "claude-3-5-sonnet"},
		},
		{
			Provider:      "9router",
			Family:        "OpenAI",
			AllowedModels: []string{"gpt-4o", "gpt-4o-mini"},
		},
	}

	// Key allowed for Claude only
	filtered := upstream.FilterPoolsByModelAccess(pools, []string{"claude-3-7-sonnet"})
	if len(filtered) != 1 || filtered[0].Family != "Claude" {
		t.Fatalf("expected only Claude pool, got: %+v", filtered)
	}

	// Wildcard allows all
	wildcard := upstream.FilterPoolsByModelAccess(pools, []string{"*"})
	if len(wildcard) != 2 {
		t.Fatalf("expected all pools on wildcard, got %d", len(wildcard))
	}
}
