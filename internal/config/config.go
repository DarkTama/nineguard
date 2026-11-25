package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port         string
	AuthEnabled  bool
	AuthFile     string
	DBFile       string
	RouterTarget string
	RouterDBFile string
	RouterAPIKey string
	CORSOrigins  []string
}

func loadDotEnv(filenames ...string) {
	for _, filename := range filenames {
		f, err := os.Open(filename)
		if err != nil {
			continue
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				if (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) ||
					(strings.HasPrefix(v, `'`) && strings.HasSuffix(v, `'`)) {
					v = v[1 : len(v)-1]
				}
				if _, exists := os.LookupEnv(k); !exists {
					_ = os.Setenv(k, v)
				}
			}
		}
	}
}

func LoadFromEnv() *Config {
	loadDotEnv(".env")

	rawTarget := getEnv("NINEGUARD_ROUTER_TARGET", "")
	routerTarget := strings.TrimRight(rawTarget, "/")

	cfg := &Config{
		Port:         getEnv("NINEGUARD_PORT", "8080"),
		AuthEnabled:  getEnvBool("NINEGUARD_AUTH_ENABLED", true),
		AuthFile:     getEnv("NINEGUARD_AUTH_FILE", "./data/auth.json"),
		DBFile:       getEnv("NINEGUARD_DB_FILE", "./data/nineguard.db"),
		RouterTarget: routerTarget,
		RouterDBFile: getEnv("NINEGUARD_ROUTER_DB_FILE", ""),
		RouterAPIKey: getEnv("NINEGUARD_ROUTER_API_KEY", ""),
		CORSOrigins:  ParseCORSOrigins(getEnv("NINEGUARD_CORS_ORIGINS", "")),
	}
	return cfg
}

// ParseCORSOrigins splits a comma-separated origin list, trimming whitespace
// and ignoring empty entries. Returns nil when no origins are configured.
func ParseCORSOrigins(raw string) []string {
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func getEnv(key, def string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return def
}
