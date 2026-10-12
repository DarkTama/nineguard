package keys_test

import (
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/traffic"
)

func TestModelQuotaAnnotateAndOverview(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	for _, q := range []string{
		`INSERT INTO models (id,name,provider_id,enabled) VALUES ('p/a','p/a','p',1),('p/b','p/b','p',1),('p/c','p/c','p',0)`,
		`INSERT INTO models (id,name,provider_id,enabled,removed_at) VALUES ('p/gone','p/gone','p',1,CURRENT_TIMESTAMP)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	mgr := keys.NewManager(d, "")
	tm := traffic.NewManager(d)

	ari, err := mgr.CreateKeyWithOptions("ari", keys.CreateKeyOptions{
		ModelAccessMode: "custom", AllowedModels: []string{"p/a", "p/b", "p/c"}, QuotaMode: "model",
		ModelQuotas: []keys.ModelQuota{
			{ModelID: "p/a", QuotaLimit: 500000, QuotaPeriod: "monthly"},
			{ModelID: "p/b", QuotaLimit: 1000000, QuotaPeriod: "daily"},
			{ModelID: "p/c", QuotaLimit: 10, QuotaPeriod: "daily"},
			{ModelID: "p/gone", QuotaLimit: 10, QuotaPeriod: "daily"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bot, _ := mgr.CreateKeyWithOptions("bot", keys.CreateKeyOptions{QuotaLimit: 5000000, QuotaPeriod: "daily"})
	_, _ = mgr.CreateKey("free", "all", nil, nil)

	rec := func(keyID, modelID string, n int) {
		if err := tm.Record(&traffic.LogEntry{APIKey: "sk-ng-test-key-1234", APIKeyID: keyID, Model: modelID, ModelID: modelID, StatusCode: 200, TotalTokens: n}); err != nil {
			t.Fatal(err)
		}
	}
	rec(ari.ID, "p/a", 512000)
	rec(ari.ID, "p/b", 860000)
	rec(bot.ID, "p/a", 4100000)

	got, err := mgr.GetKey(ari.ID)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]keys.ModelQuota{}
	for _, r := range got.ModelQuotas {
		by[r.ModelID] = r
	}
	if r := by["p/a"]; r.Percent != 102 || r.Status != "exhausted" || r.Usage != 512000 || r.ResetAt == nil {
		t.Errorf("p/a: %+v", r)
	}
	if r := by["p/b"]; r.Percent != 86 || r.Status != "near" {
		t.Errorf("p/b: %+v", r)
	}
	if r := by["p/c"]; !r.Inactive || r.InactiveReason != "disabled" || r.Status != "inactive" {
		t.Errorf("p/c: %+v", r)
	}
	if r := by["p/gone"]; !r.Inactive || r.InactiveReason != "removed" {
		t.Errorf("p/gone: %+v", r)
	}
	s := got.ModelQuotaSummary
	if s == nil || s.Count != 4 || s.Active != 2 || s.TopModelID != "p/a" || s.TopPercent != 102 {
		t.Errorf("summary: %+v", s)
	}

	// total period has no reset instant
	totalKey, _ := mgr.CreateKeyWithOptions("life", keys.CreateKeyOptions{QuotaMode: "model", ModelQuotas: []keys.ModelQuota{{ModelID: "p/a", QuotaLimit: 5, QuotaPeriod: "total"}}})
	if g, _ := mgr.GetKey(totalKey.ID); g.ModelQuotas[0].ResetAt != nil {
		t.Error("total period must have a nil reset_at")
	}

	ov, err := mgr.QuotaOverview(80, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ov.Keys[0].Name != "ari" || ov.Keys[1].Name != "bot" || ov.Keys[0].TopPercent != 102 || ov.Keys[1].TopPercent != 82 {
		t.Fatalf("order: %s(%d) %s(%d)", ov.Keys[0].Name, ov.Keys[0].TopPercent, ov.Keys[1].Name, ov.Keys[1].TopPercent)
	}
	if ov.Keys[1].KeyQuota == nil || ov.Keys[1].Status != "near" || len(ov.Keys[1].ModelQuotas) != 0 {
		t.Errorf("bot: %+v", ov.Keys[1])
	}
	if ov.Keys[0].KeyQuota != nil || ov.Keys[0].ModelQuotas[len(ov.Keys[0].ModelQuotas)-1].Inactive == false {
		t.Errorf("inactive rows must sort last: %+v", ov.Keys[0].ModelQuotas)
	}
	sm := ov.Summary
	if sm.NearLimit != 2 || sm.Exhausted != 1 || sm.ModelQuotas != 5 || sm.KeysWithQuota != 3 {
		t.Errorf("summary: %+v", sm)
	}
	last := ov.Keys[len(ov.Keys)-1]
	if last.Status != "unlimited" && ov.Keys[3].Status != "unlimited" {
		t.Errorf("unlimited key should sort last: %+v", ov.Keys)
	}

	// List responses carry the summary only.
	list, _ := mgr.ListKeys()
	for _, k := range list {
		if k.ID == ari.ID && (k.ModelQuotas != nil || k.ModelQuotaSummary == nil || k.ModelQuotaSummary.TopPercent != 102) {
			t.Errorf("list entry: %+v", k)
		}
	}

	// no_access: drop p/b from the allow-list without touching the rows.
	if _, err := mgr.UpdateKeyAll(ari.ID, keys.KeyUpdate{Name: "ari", ModelAccessMode: "custom", AllowedModels: []string{"p/a", "p/c"}}); err != nil {
		t.Fatal(err)
	}
	got, _ = mgr.GetKey(ari.ID)
	for _, r := range got.ModelQuotas {
		if r.ModelID == "p/b" && (!r.Inactive || r.InactiveReason != "no_access") {
			t.Errorf("p/b after access change: %+v", r)
		}
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM models WHERE id NOT IN ('p/a','p/b','p/c','p/gone')`).Scan(&n)
	if n != 0 {
		t.Error("read paths must not auto-register models")
	}
}
