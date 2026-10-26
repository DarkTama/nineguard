package providers_test

import (
	"nineguard/internal/db"
	"nineguard/internal/providers"
	"path/filepath"
	"testing"
)

func TestProviderBackendMigrationAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.db")
	d, err := db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	m := providers.NewManager(d)
	p, err := m.CreateProvider("Legacy", "https://example.test/v1", "fake-key", "legacy", true, true)
	if err != nil {
		t.Fatal(err)
	}
	id := p.ID
	d.Close()
	for range 2 {
		d, err = db.InitDB(path)
		if err != nil {
			t.Fatal(err)
		}
		m = providers.NewManager(d)
		p, err = m.GetProvider(id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Backend != "http" || p.Route != "https://example.test/v1" || p.APIKey != "fake-key" {
			t.Fatalf("legacy changed: %+v", p)
		}
		d.Close()
	}
	d, err = db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m = providers.NewManager(d)
	str := func(s string) *string { return &s }
	p, err = m.CreateProviderWithOptions(providers.ProviderOptions{Name: "Claude", Prefix: "claude", Backend: str("cliproxy"), Family: str("claude"), IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Backend != "cliproxy" || p.Family != "claude" || p.Route != "" {
		t.Fatalf("SDK identity: %+v", p)
	}
	p, err = m.UpdateProviderWithOptions(p.ID, providers.ProviderOptions{Name: "Renamed", Prefix: "claude", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Backend != "cliproxy" || p.Family != "claude" {
		t.Fatalf("identity lost: %+v", p)
	}
	list, err := m.ListProviders()
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v %v", list, err)
	}
	for _, o := range []providers.ProviderOptions{
		{Name: "Bad", Route: "https://evil.test", Backend: str("cliproxy"), Family: str("claude")},
		{Name: "Bad", Backend: str("unknown")},
		{Name: "Bad", Backend: str("cliproxy"), Family: str("unknown")},
	} {
		if _, err = m.CreateProviderWithOptions(o); err == nil {
			t.Fatalf("accepted invalid provider: %+v", o)
		}
	}
}
