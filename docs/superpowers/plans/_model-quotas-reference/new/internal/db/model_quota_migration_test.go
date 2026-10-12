package db_test

import (
	"path/filepath"
	"testing"

	"nineguard/internal/db"
)

func TestModelQuotaSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mq.db")
	d, err := db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	d, err = db.InitDB(path) // second run: migration must be idempotent
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.Exec(`INSERT INTO api_keys (id,key,prefix,name) VALUES ('k1','sk','p','n')`); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := d.QueryRow(`SELECT quota_mode FROM api_keys WHERE id='k1'`).Scan(&mode); err != nil || mode != "key" {
		t.Fatalf("quota_mode default: %q %v", mode, err)
	}
	if _, err := d.Exec(`INSERT INTO api_key_model_quotas (api_key_id,model_id,quota_limit,quota_period) VALUES ('k1','P/A',10,'daily')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO api_key_model_quotas (api_key_id,model_id,quota_limit,quota_period) VALUES ('k1','p/a',10,'daily')`); err == nil {
		t.Fatal("expected case-insensitive primary key conflict")
	}
	if _, err := d.Exec(`INSERT INTO api_key_model_quotas (api_key_id,model_id,quota_limit,quota_period) VALUES ('k1','p/b',0,'daily')`); err == nil {
		t.Fatal("expected CHECK failure for limit 0")
	}
	if _, err := d.Exec(`INSERT INTO api_key_model_quotas (api_key_id,model_id,quota_limit,quota_period) VALUES ('k1','p/c',5,'none')`); err == nil {
		t.Fatal("expected CHECK failure for period none")
	}
	if _, err := d.Exec(`INSERT INTO traffic_logs (model,status_code,api_key_id,model_id) VALUES ('m',200,'k1','P/A')`); err != nil {
		t.Fatalf("traffic_logs.model_id: %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_traffic_key_model_ts'`).Scan(&n)
	if n != 1 {
		t.Fatal("index idx_traffic_key_model_ts missing")
	}
	var warn string
	if err := d.QueryRow(`SELECT value FROM settings WHERE key='quota_warn_percent'`).Scan(&warn); err != nil || warn != "80" {
		t.Fatalf("quota_warn_percent: %q %v", warn, err)
	}
}
