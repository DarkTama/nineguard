package providers

import (
	"fmt"
	"net/url"
	"strings"
)

// IconPresets are the brand marks the dashboard bundles (web/static/js/providericons.js).
// Keep both lists in sync. An empty icon means "auto": the UI guesses from route and name.
var IconPresets = []string{"9router", "omniroute", "nineguard", "claude", "anthropic", "openai", "openrouter", "ollama", "gemini", "generic"}

const maxIconURLLen = 512

// normalizeIcon validates a provider icon: "" (auto), a preset id, or an http(s) image URL.
// Other schemes (javascript:, data:) are rejected because the value ends up in an <img src>.
func normalizeIcon(raw string) (string, error) {
	icon := strings.TrimSpace(raw)
	if icon == "" {
		return "", nil
	}
	lower := strings.ToLower(icon)
	for _, p := range IconPresets {
		if lower == p {
			return p, nil
		}
	}
	if len(icon) > maxIconURLLen {
		return "", fmt.Errorf("icon URL is longer than %d characters", maxIconURLLen)
	}
	u, err := url.Parse(icon)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("icon must be a preset (%s) or an http(s) image URL", strings.Join(IconPresets, ", "))
	}
	return icon, nil
}
