package providers_test

import (
	"testing"

	"nineguard/internal/db"
	"nineguard/internal/providers"
)

func TestResolveModelID(t *testing.T) {
	d, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	pm := providers.NewManager(d)

	if _, _, _, err := pm.ResolveModelID("gpt-4o"); err == nil {
		t.Fatal("expected error with no providers")
	}
	if _, err := pm.CreateProvider("Router", "http://x", "", "9router", true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.CreateProvider("OpenAI", "http://y", "", "openai", false, true); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ in, actual, id string }{
		{"9router/gpt-4o", "gpt-4o", "9router/gpt-4o"},
		{"gpt-4o", "gpt-4o", "9router/gpt-4o"}, // bare -> default provider
		{"openai/gpt-4o", "gpt-4o", "openai/gpt-4o"},
		{"openai/ft:abc/def", "ft:abc/def", "openai/ft:abc/def"},
		{"", "", ""}, // no model: no id
	}
	for _, c := range cases {
		_, actual, id, err := pm.ResolveModelID(c.in)
		if err != nil || actual != c.actual || id != c.id {
			t.Errorf("%q -> (%q,%q,%v), want (%q,%q)", c.in, actual, id, err, c.actual, c.id)
		}
	}
	if !pm.IsCanonicalModelID("openai/GPT-4o") {
		t.Error("openai/GPT-4o should be canonical (model part is case-insensitive)")
	}
	// The provider prefix is matched case-sensitively when routing, so OPENAI/... goes to the
	// default provider and is not the canonical form of an openai model.
	if pm.IsCanonicalModelID("OPENAI/gpt-4o") {
		t.Error("OPENAI/gpt-4o routes to the default provider, so it is not canonical")
	}
	if pm.IsCanonicalModelID("gpt-4o") {
		t.Error("bare gpt-4o is not canonical when default provider has a prefix")
	}
}

func TestResolveModelIDNoPrefixProvider(t *testing.T) {
	d, _ := db.InitDB(":memory:")
	defer d.Close()
	pm := providers.NewManager(d)
	_, _ = pm.CreateProvider("Plain", "http://x", "", "", true, true)
	_, _, id, err := pm.ResolveModelID("gpt-4o")
	if err != nil || id != "gpt-4o" {
		t.Fatalf("got %q %v", id, err)
	}
	if !pm.IsCanonicalModelID("gpt-4o") {
		t.Error("gpt-4o canonical when default provider has no prefix")
	}
}
