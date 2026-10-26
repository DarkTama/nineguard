package providerengine

import (
	"context"
	"encoding/json"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"nineguard/internal/providers"
	"strings"
	"testing"
	"time"
)

func TestSDKQuotaAccountIsolation(t *testing.T) {
	s, d := testStore(t)
	a := grantRecord(t, s)
	if _, err := s.Save(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	e, err := New(d, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	observed := time.Now().UTC()
	current, _ := e.manager.GetByID(a.ID)
	_, _, err = e.manager.Modify(context.Background(), a.ID, current.RegistrationEpoch, func(record *coreauth.Auth) error {
		record.Quota.Signals = map[string]string{"Anthropic-Ratelimit-Unified-5h-Utilization": "0.53"}
		record.Quota.ObservedAt = observed
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p := providers.Provider{ID: "p", Name: "Claude", Backend: "cliproxy", Family: "claude", QuotaType: "disabled"}
	snapshot, err := e.SDKQuotas(context.Background(), p, observed)
	if err != nil || len(snapshot.Connections) != 1 {
		t.Fatalf("snapshot %v %v", snapshot, err)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "fake-access") || strings.Contains(string(raw), "refresh_token") {
		t.Fatal("SDK credential leaked")
	}
	p.ID = "other"
	snapshot, err = e.SDKQuotas(context.Background(), p, observed)
	if err != nil || len(snapshot.Connections) != 0 {
		t.Fatal("account escaped provider")
	}
	if err = e.SetDisabled(context.Background(), "p", a.ID, true); err != nil {
		t.Fatal(err)
	}
	p.ID = "p"
	snapshot, err = e.SDKQuotas(context.Background(), p, observed)
	if err != nil || len(snapshot.Connections) != 0 {
		t.Fatal("disabled account counted")
	}
	if err = e.Disconnect(context.Background(), "p", a.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = e.SDKQuotas(context.Background(), p, observed)
	if err != nil || len(snapshot.Connections) != 0 {
		t.Fatal("disconnected account counted")
	}
}
