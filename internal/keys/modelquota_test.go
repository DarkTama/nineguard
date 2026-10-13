package keys_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
)

func TestQuotaHelpers(t *testing.T) {
	if m, ok := keys.NormalizeQuotaMode(""); m != "key" || !ok {
		t.Error("empty -> key")
	}
	if m, ok := keys.NormalizeQuotaMode(" MODEL "); m != "model" || !ok {
		t.Error("MODEL -> model")
	}
	if _, ok := keys.NormalizeQuotaMode("x"); ok {
		t.Error("x invalid")
	}
	if keys.QuotaPercent(512000, 500000) != 102 || keys.QuotaPercent(5, 0) != 0 {
		t.Error("percent")
	}
	for _, c := range []struct {
		u, l int64
		w    int
		want string
	}{{100, 100, 80, "exhausted"}, {80, 100, 80, "near"}, {79, 100, 80, "ok"}} {
		if got := keys.QuotaStatus(c.u, c.l, c.w); got != c.want {
			t.Errorf("status(%d,%d,%d)=%s want %s", c.u, c.l, c.w, got, c.want)
		}
	}
	if keys.IsModelQuotaPeriod("none") || !keys.IsModelQuotaPeriod("total") {
		t.Error("periods")
	}
}

func TestModelQuotaReadPath(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	mgr := keys.NewManager(d, "")
	k, err := mgr.CreateKey("a", "all", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if k.QuotaMode != "key" {
		t.Fatalf("new key mode %q", k.QuotaMode)
	}
	_, _ = d.Exec(`UPDATE api_keys SET quota_mode='model' WHERE id=?`, k.ID)
	_, _ = d.Exec(`INSERT INTO api_key_model_quotas (api_key_id,model_id,quota_limit,quota_period) VALUES (?, 'p/a', 100, 'daily')`, k.ID)

	// Fresh manager loads the cache from the database.
	mgr = keys.NewManager(d, "")
	ki, ok := mgr.ValidateClientKey(k.RawKey)
	if !ok || !ki.EnforcesModelQuotas() || ki.ModelQuotaFor("P/A") == nil || ki.ModelQuotaFor("p/z") != nil {
		t.Fatalf("cache path: %+v", ki)
	}

	// Fallback path: a row inserted behind the cache's back.
	_, _ = d.Exec(`INSERT INTO api_keys (id,key,prefix,name,is_active,quota_limit,quota_period,quota_mode) VALUES ('k2','sk-ng-fallback','sk','b',1,50,'weekly','model')`)
	_, _ = d.Exec(`INSERT INTO api_key_model_quotas (api_key_id,model_id,quota_limit,quota_period) VALUES ('k2','p/b',7,'total')`)
	ki2, ok := mgr.ValidateClientKey("sk-ng-fallback")
	if !ok || ki2.QuotaMode != "model" || ki2.QuotaLimit != 50 || ki2.QuotaPeriod != "weekly" || len(ki2.ModelQuotas) != 1 {
		t.Fatalf("fallback path: %+v", ki2)
	}

	got, err := mgr.GetKey(k.ID)
	if err != nil || got.QuotaMode != "model" || len(got.ModelQuotas) != 1 {
		t.Fatalf("GetKey: %+v %v", got, err)
	}

	if err := mgr.DeleteKey(k.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM api_key_model_quotas WHERE api_key_id=?`, k.ID).Scan(&n)
	if n != 0 {
		t.Fatal("DeleteKey must remove Model Quotas")
	}
}
