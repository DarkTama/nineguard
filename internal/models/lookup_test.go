package models_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/models"
)

func TestLookupStatus(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	_, _ = d.Exec(`INSERT INTO models (id,name,provider_id,enabled) VALUES ('p/on','p/on','p',1),('p/off','p/off','p',0)`)
	_, _ = d.Exec(`INSERT INTO models (id,name,provider_id,enabled,removed_at) VALUES ('p/gone','p/gone','p',1,CURRENT_TIMESTAMP)`)

	f, e, r := models.LookupStatus(d, "P/ON")
	if !f || !e || r {
		t.Errorf("on: %v %v %v", f, e, r)
	}
	f, e, r = models.LookupStatus(d, "p/off")
	if !f || e || r {
		t.Errorf("off: %v %v %v", f, e, r)
	}
	f, _, r = models.LookupStatus(d, "p/gone")
	if !f || !r {
		t.Errorf("gone: %v %v", f, r)
	}
	if f, _, _ = models.LookupStatus(d, "p/unknown"); f {
		t.Error("unknown must be not found")
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM models WHERE id='p/unknown'`).Scan(&n)
	if n != 0 {
		t.Error("LookupStatus must not auto-register")
	}
}
