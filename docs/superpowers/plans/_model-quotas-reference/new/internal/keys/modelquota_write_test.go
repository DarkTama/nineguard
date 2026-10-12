package keys_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/keys"
	"nineguard/internal/models"
)

func TestCreateAndUpdateModelQuotas(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	mgr := keys.NewManager(d, "")

	k, err := mgr.CreateKeyWithOptions("ari", keys.CreateKeyOptions{
		QuotaLimit: 5000, QuotaPeriod: "daily", QuotaMode: "model",
		ModelQuotas: []keys.ModelQuota{
			{ModelID: "p/a", QuotaLimit: 100, QuotaPeriod: "daily"},
			{ModelID: "p/b", QuotaLimit: 200, QuotaPeriod: "weekly"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if k.QuotaMode != "model" || len(k.ModelQuotas) != 2 {
		t.Fatalf("created: %+v", k)
	}
	if ki, ok := mgr.ValidateClientKey(k.RawKey); !ok || !ki.EnforcesModelQuotas() || ki.ModelQuotaFor("p/b") == nil {
		t.Fatalf("proxy cache missing model quotas: %+v", ki)
	}
	if _, err := mgr.CreateKeyWithOptions("bad", keys.CreateKeyOptions{QuotaMode: "x"}); err == nil {
		t.Fatal("invalid quota_mode must fail")
	}

	// nil ModelQuotas keeps rows.
	got, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "ari2", ModelAccessMode: "all"})
	if err != nil || got.Name != "ari2" || len(got.ModelQuotas) != 2 {
		t.Fatalf("keep: %+v %v", got, err)
	}
	// Switching mode keeps the Key Quota and the rows (ADR 0007: inactive mode is kept).
	keyMode := "key"
	got, err = mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "ari2", ModelAccessMode: "all", QuotaMode: &keyMode})
	if err != nil || got.QuotaMode != "key" || got.QuotaLimit != 5000 || got.QuotaPeriod != "daily" || len(got.ModelQuotas) != 2 {
		t.Fatalf("mode switch: %+v %v", got, err)
	}
	// Replace.
	repl := []keys.ModelQuota{{ModelID: "p/c", QuotaLimit: 9, QuotaPeriod: "total"}}
	got, err = mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "ari2", ModelAccessMode: "all", ModelQuotas: &repl})
	if err != nil || len(got.ModelQuotas) != 1 || got.ModelQuotas[0].ModelID != "p/c" {
		t.Fatalf("replace: %+v %v", got, err)
	}
	// Clear with an explicit empty slice.
	empty := []keys.ModelQuota{}
	got, err = mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "ari2", ModelAccessMode: "all", ModelQuotas: &empty})
	if err != nil || len(got.ModelQuotas) != 0 {
		t.Fatalf("clear: %+v %v", got, err)
	}
}

func TestUpdateKeyAllRollsBack(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	mgr := keys.NewManager(d, "")
	k, _ := mgr.CreateKey("orig", "all", nil, nil)

	dup := []keys.ModelQuota{
		{ModelID: "p/a", QuotaLimit: 1, QuotaPeriod: "daily"},
		{ModelID: "P/A", QuotaLimit: 2, QuotaPeriod: "daily"}, // same id, other case
	}
	if _, err := mgr.UpdateKeyAll(k.ID, keys.KeyUpdate{Name: "changed", ModelAccessMode: "all", ModelQuotas: &dup}); err == nil {
		t.Fatal("duplicate rows must fail")
	}
	got, _ := mgr.GetKey(k.ID)
	if got.Name != "orig" {
		t.Fatalf("key row must roll back with the quota rows, name=%q", got.Name)
	}
	if _, err := mgr.UpdateKeyAll("missing", keys.KeyUpdate{Name: "x"}); err == nil {
		t.Fatal("unknown key must fail")
	}
}

func TestAccessPreviewAndDeriveMode(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	mgr := keys.NewManager(d, "")
	mm := models.NewManager(d)
	g, err := mm.CreateGroup("G", "", []string{"p/a", "q/*"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.ReloadGroupCache()

	pv := mgr.AccessPreview("group", []string{g.ID}, nil)
	if !pv.IsModelAllowed("p/a") || !pv.IsModelAllowed("q/zzz") || pv.IsModelAllowed("p/b") {
		t.Fatal("group preview")
	}
	if !mgr.AccessPreview("all", nil, nil).IsModelAllowed("anything") {
		t.Fatal("all preview")
	}
	if keys.DeriveAccessMode("", []string{"g"}, nil) != "group" || keys.DeriveAccessMode("", nil, []string{"m"}) != "custom" ||
		keys.DeriveAccessMode("", nil, nil) != "all" || keys.DeriveAccessMode("Custom", nil, nil) != "custom" {
		t.Fatal("derive")
	}
}
