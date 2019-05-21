package zeroscaler

import (
	"time"

	"github.com/deislabs/osiris/pkg/metrics"
)

type podStats struct {
	podDeletedTime *time.Time
	prevStatTime   *time.Time
	prevStats      *metrics.ProxyConnectionStats
	recentStatTime *time.Time
	recentStats    *metrics.ProxyConnectionStats
}
