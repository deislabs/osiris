package zeroscaler

import (
	"encoding/json"
	"fmt"

	"github.com/deislabs/osiris/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
)

type metricsScraperConfig struct {
	ScraperName    string          `json:"type"`
	Implementation json.RawMessage `json:"implementation"`
}

type metricsScraper interface {
	Scrap(pod *corev1.Pod) *metrics.ProxyConnectionStats
}

func newMetricsScraper(config metricsScraperConfig) (metricsScraper, error) {
	var (
		scraper metricsScraper
		err     error
	)
	switch config.ScraperName {
	case prometheusScraperName:
		scraper, err = newPrometheusScraper(config)
	case osirisScraperName:
		scraper = newOsirisScraper()
	default:
		return nil, fmt.Errorf("unknown scraper %s", config.ScraperName)
	}
	return scraper, err
}
