package providerengine

import (
	"github.com/sirupsen/logrus"
	"testing"
)

func TestPrivateEngineLogsRemoveOAuthCapabilities(t *testing.T) {
	entry := &logrus.Entry{Message: `200 GET /codex/callback?code=secret-code&state=secret-state`, Data: logrus.Fields{"error": "refresh_token=secret-refresh"}}
	if err := (privateLogFilter{}).Fire(entry); err != nil {
		t.Fatal(err)
	}
	if entry.Message != "embedded provider authentication event" || len(entry.Data) != 0 {
		t.Fatalf("OAuth capabilities leaked: %+v", entry)
	}
}
