package providerengine

import "time"

// Account is the only account representation sent to the dashboard. Never embed SDK Auth here.
type Account struct {
	ID              string     `json:"id"`
	ProviderID      string     `json:"provider_id"`
	Family          string     `json:"family"`
	Label           string     `json:"label"`
	Status          string     `json:"status"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	LastRefreshedAt *time.Time `json:"last_refreshed_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
}

type Connector struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Protocols  []string `json:"protocols"`
	LoginModes []string `json:"login_modes"`
	Icon       string   `json:"icon"`
}

type LoginSession struct {
	ID               string    `json:"id"`
	ProviderID       string    `json:"provider_id"`
	AccountID        string    `json:"account_id,omitempty"`
	State            string    `json:"state"`
	AuthorizationURL string    `json:"authorization_url,omitempty"`
	UserCode         string    `json:"user_code,omitempty"`
	VerificationURL  string    `json:"verification_url,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	Error            string    `json:"error,omitempty"`
}
