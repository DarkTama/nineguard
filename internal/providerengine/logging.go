package providerengine

import (
	"github.com/sirupsen/logrus"
	"strings"
)

// The embedded SDK shares logrus with its executors. Never forward OAuth capabilities or
// upstream error bodies from that private engine; NineGuard records sanitized operation errors.
type privateLogFilter struct{}

func (privateLogFilter) Levels() []logrus.Level { return logrus.AllLevels }
func (privateLogFilter) Fire(entry *logrus.Entry) error {
	lower := strings.ToLower(entry.Message)
	sensitive := false
	for _, marker := range []string{"callback", "oauth", "token", "authorization", "auth-status", "oauth-session"} {
		if strings.Contains(lower, marker) {
			sensitive = true
			break
		}
	}
	if sensitive {
		entry.Message = "embedded provider authentication event"
		entry.Data = logrus.Fields{}
	}
	return nil
}
