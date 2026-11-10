package source

import (
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
)

func defaultTestConfig() config.Config { return config.Default() }

func srcCfg(enabled bool, interval time.Duration) config.SourceConfig {
	return config.SourceConfig{Enabled: enabled, Interval: interval}
}
