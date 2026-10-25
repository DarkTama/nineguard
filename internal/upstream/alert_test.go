package upstream

import (
	"testing"
	"time"
)

func TestQuotaAlerter_FirstSeenWarningAlertsOnce(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	res := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets: []QuotaBucket{
					{
						Name:                "5h Limit",
						RemainingPercentage: 20,
						Warning:             true,
					},
				},
			},
		},
	}

	msgs := alerter.Observe(res, now)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 alert on first-seen warning, got %d: %v", len(msgs), msgs)
	}
	expectedMsg := "upstream_quota_low: upstream quota running low"
	if msgs[0] != expectedMsg {
		t.Errorf("expected alert message %q, got %q", expectedMsg, msgs[0])
	}
}

func TestQuotaAlerter_RepeatedObserveNoAlert(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	res := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets: []QuotaBucket{
					{
						Name:                "5h Limit",
						RemainingPercentage: 15,
						Warning:             true,
					},
				},
			},
		},
	}

	// First observation alerts
	msgs1 := alerter.Observe(res, now)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 alert on first-seen, got %d", len(msgs1))
	}

	// Second observation with same state should NOT alert
	msgs2 := alerter.Observe(res, now.Add(5*time.Minute))
	if len(msgs2) != 0 {
		t.Errorf("expected 0 alerts on repeated observe, got %d: %v", len(msgs2), msgs2)
	}
}

func TestQuotaAlerter_RecoveryEmitsRecovered(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	resWarn := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets: []QuotaBucket{
					{
						Name:                "Weekly Limit",
						RemainingPercentage: 10,
						Warning:             true,
					},
				},
			},
		},
	}

	// Trigger warning
	msgs1 := alerter.Observe(resWarn, now)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(msgs1))
	}

	// Recover (Warning = false)
	resOk := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets: []QuotaBucket{
					{
						Name:                "Weekly Limit",
						RemainingPercentage: 100,
						Warning:             false,
					},
				},
			},
		},
	}

	msgs2 := alerter.Observe(resOk, now.Add(10*time.Minute))
	if len(msgs2) != 1 {
		t.Fatalf("expected 1 recovery alert, got %d: %v", len(msgs2), msgs2)
	}
	expected := "upstream_quota_recovered: upstream quota recovered"
	if msgs2[0] != expected {
		t.Errorf("expected message %q, got %q", expected, msgs2[0])
	}

	// Repeated observe while recovered should not alert
	msgs3 := alerter.Observe(resOk, now.Add(15*time.Minute))
	if len(msgs3) != 0 {
		t.Errorf("expected 0 alerts on repeated recovered state, got %d: %v", len(msgs3), msgs3)
	}
}

func TestQuotaAlerter_CreditExhausted(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	resCredit := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets: []QuotaBucket{
					{
						Name:            "Credits",
						IsCreditBalance: true,
						Remaining:       0,
						Warning:         true,
					},
				},
			},
		},
	}

	msgs1 := alerter.Observe(resCredit, now)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 alert for credit exhausted, got %d: %v", len(msgs1), msgs1)
	}
	expected := "upstream_quota_low: upstream credit balance exhausted"
	if msgs1[0] != expected {
		t.Errorf("expected message %q, got %q", expected, msgs1[0])
	}

	// Recovery
	resCreditRecovered := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets: []QuotaBucket{
					{
						Name:            "Credits",
						IsCreditBalance: true,
						Remaining:       5000,
						Warning:         false,
					},
				},
			},
		},
	}

	msgs2 := alerter.Observe(resCreditRecovered, now.Add(5*time.Minute))
	if len(msgs2) != 1 {
		t.Fatalf("expected 1 recovery alert, got %d: %v", len(msgs2), msgs2)
	}
	expectedRecovered := "upstream_quota_recovered: upstream credit balance restored"
	if msgs2[0] != expectedRecovered {
		t.Errorf("expected message %q, got %q", expectedRecovered, msgs2[0])
	}
}

func TestQuotaAlerter_ConnectionErrorAndRecovery(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	resErr := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "error",
				ErrorMsg:     "upstream dial timeout",
				Buckets:      []QuotaBucket{},
			},
		},
	}

	msgs1 := alerter.Observe(resErr, now)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 alert for connection error, got %d: %v", len(msgs1), msgs1)
	}
	expectedErr := "upstream_connection_error: upstream connection error"
	if msgs1[0] != expectedErr {
		t.Errorf("expected message %q, got %q", expectedErr, msgs1[0])
	}

	// Repeated error should not alert
	msgs2 := alerter.Observe(resErr, now.Add(2*time.Minute))
	if len(msgs2) != 0 {
		t.Errorf("expected 0 alerts on repeated connection error, got %d", len(msgs2))
	}

	// Recovery to ok
	resOk := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "ok",
				Buckets:      []QuotaBucket{},
			},
		},
	}

	msgs3 := alerter.Observe(resOk, now.Add(5*time.Minute))
	if len(msgs3) != 1 {
		t.Fatalf("expected 1 recovery alert, got %d: %v", len(msgs3), msgs3)
	}
	expectedRecovered := "upstream_connection_recovered: upstream connection recovered"
	if msgs3[0] != expectedRecovered {
		t.Errorf("expected message %q, got %q", expectedRecovered, msgs3[0])
	}
}

func TestQuotaAlerter_DegradedNoAlert(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	resDegraded := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "9router",
		Status:       "ok",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "degraded",
				ErrorMsg:     "stale cached limits",
				Buckets: []QuotaBucket{
					{
						Name:                "5h Limit",
						RemainingPercentage: 80,
						Warning:             false,
					},
				},
			},
		},
	}

	msgs := alerter.Observe(resDegraded, now)
	if len(msgs) != 0 {
		t.Errorf("expected 0 alerts for degraded connection, got %d: %v", len(msgs), msgs)
	}
}

func TestQuotaAlerter_UnsupportedIgnored(t *testing.T) {
	alerter := NewQuotaAlerter()
	now := time.Now()

	resUnsupported := &ProviderQuotaResult{
		ProviderID:   "prov_1",
		ProviderName: "Provider 1",
		QuotaType:    "disabled",
		Status:       "unsupported",
		Connections: []ConnectionQuota{
			{
				ConnectionID: "conn_1",
				AccountLabel: "user@example.com",
				Status:       "error",
				Buckets: []QuotaBucket{
					{
						Name:    "5h Limit",
						Warning: true,
					},
				},
			},
		},
	}

	msgs := alerter.Observe(resUnsupported, now)
	if len(msgs) != 0 {
		t.Errorf("expected 0 alerts for unsupported provider, got %d: %v", len(msgs), msgs)
	}
}

func TestQuotaAlerter_NilSafe(t *testing.T) {
	now := time.Now()

	// Nil alerter
	var nilAlerter *QuotaAlerter
	msgs1 := nilAlerter.Observe(&ProviderQuotaResult{}, now)
	if len(msgs1) != 0 {
		t.Errorf("expected 0 alerts on nil alerter, got %d", len(msgs1))
	}

	// Nil result
	alerter := NewQuotaAlerter()
	msgs2 := alerter.Observe(nil, now)
	if len(msgs2) != 0 {
		t.Errorf("expected 0 alerts on nil result, got %d", len(msgs2))
	}
}
