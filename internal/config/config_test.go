package config

import (
	"reflect"
	"testing"
)

func TestParseCORSOrigins(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "comma separated with spaces and empty entries",
			raw:  "a, b,,c ",
			want: []string{"a", "b", "c"},
		},
		{
			name: "empty string",
			raw:  "",
			want: nil,
		},
		{
			name: "whitespace and commas only",
			raw:  " , , ",
			want: nil,
		},
		{
			name: "single origin trimmed",
			raw:  "  https://app.example.com  ",
			want: []string{"https://app.example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCORSOrigins(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseCORSOrigins(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadFromEnvCORSOrigins(t *testing.T) {
	t.Setenv("NINEGUARD_CORS_ORIGINS", " https://app.example.com , http://localhost:5173 ")
	cfg := LoadFromEnv()
	want := []string{"https://app.example.com", "http://localhost:5173"}
	if !reflect.DeepEqual(cfg.CORSOrigins, want) {
		t.Fatalf("cfg.CORSOrigins = %#v, want %#v", cfg.CORSOrigins, want)
	}
}
