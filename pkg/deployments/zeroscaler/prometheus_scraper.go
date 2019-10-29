package zeroscaler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/deislabs/osiris/pkg/metrics"
	"github.com/golang/glog"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
)

const (
	prometheusScraperName = "prometheus"
)

type prometheusScraperConfig struct {
	Port                          int               `json:"port"`
	Path                          string            `json:"path"`
	OpenedConnectionsMetricName   string            `json:"openedConnectionsMetricName"`   // nolint: lll
	OpenedConnectionsMetricLabels map[string]string `json:"openedConnectionsMetricLabels"` // nolint: lll
	ClosedConnectionsMetricName   string            `json:"closedConnectionsMetricName"`   // nolint: lll
	ClosedConnectionsMetricLabels map[string]string `json:"closedConnectionsMetricLabels"` // nolint: lll
}

// prometheusScraper is a metrics scraper that scraps metrics exposed by
// the pods using the prometheus format. It expects the scraped pods to
// expose an HTTP endpoint at a given port/path with the metrics in the
// prometheus format.
type prometheusScraper struct {
	httpClient *http.Client
	config     prometheusScraperConfig
}

func newPrometheusScraper(
	config metricsScraperConfig,
) (*prometheusScraper, error) {
	var cfg prometheusScraperConfig
	if err := json.Unmarshal(config.Implementation, &cfg); err != nil {
		return nil, fmt.Errorf(
			"invalid prometheus configuration: %s",
			err,
		)
	}

	// check for missing values
	if cfg.Port == 0 {
		return nil, errors.New(
			"Prometheus metrics can't be scraped: missing port",
		)
	}
	if len(cfg.OpenedConnectionsMetricName) == 0 {
		return nil, errors.New(
			"Prometheus metrics can't be scraped: " +
				"missing openedConnectionsMetricName",
		)
	}
	if len(cfg.ClosedConnectionsMetricName) == 0 {
		return nil, errors.New(
			"Prometheus metrics can't be scraped: " +
				"missing closedConnectionsMetricName",
		)
	}

	// default values
	if len(cfg.Path) == 0 {
		cfg.Path = "/metrics"
	}

	return &prometheusScraper{
		config: cfg,
		// A very aggressive timeout. When collecting metrics, we want to do it very
		// quickly to minimize the possibility that some pods we've checked on have
		// served requests while we've been checking on OTHER pods.
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}, nil
}

func (s *prometheusScraper) Scrap(
	pod *corev1.Pod,
) *metrics.ProxyConnectionStats {
	target := fmt.Sprintf(
		"http://%s:%d%s",
		pod.Status.PodIP,
		s.config.Port,
		s.config.Path,
	)

	resp, err := s.httpClient.Get(target)
	if err != nil {
		glog.Errorf("Error requesting metrics from %s: %s", target, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		glog.Errorf(
			"Received unexpected HTTP response code %d when requesting metrics "+
				"from %s",
			resp.StatusCode,
			target,
		)
		return nil
	}

	var (
		decoder = expfmt.NewDecoder(
			resp.Body,
			expfmt.ResponseFormat(resp.Header),
		)
		pcs = metrics.ProxyConnectionStats{
			ProxyID: string(pod.UID),
		}
		metricFamily                   io_prometheus_client.MetricFamily
		openedConnectionsMetricScraped bool
		closedConnectionsMetricScraped bool
	)
	for {
		// the decoder decodes metricFamilies 1 by 1
		// and finishes with an io.EOF error
		err = decoder.Decode(&metricFamily)
		if err == io.EOF {
			break
		}
		if err != nil {
			glog.Errorf("Error decoding prometheus metrics from %s: %s", target, err)
			return nil
		}

		if metricFamily.GetName() == s.config.OpenedConnectionsMetricName &&
			!openedConnectionsMetricScraped {
			value, found := extractPrometheusMetricFamilyValue(
				metricFamily,
				s.config.OpenedConnectionsMetricLabels,
			)
			if found {
				pcs.ConnectionsOpened = value
				openedConnectionsMetricScraped = true
			}
		}
		if metricFamily.GetName() == s.config.ClosedConnectionsMetricName &&
			!closedConnectionsMetricScraped {
			value, found := extractPrometheusMetricFamilyValue(
				metricFamily,
				s.config.ClosedConnectionsMetricLabels,
			)
			if found {
				pcs.ConnectionsClosed = value
				closedConnectionsMetricScraped = true
			}
		}

		if openedConnectionsMetricScraped && closedConnectionsMetricScraped {
			break
		}
	}

	// make sure not to return a half-valid object
	if !openedConnectionsMetricScraped {
		glog.Errorf(
			"Prometheus-scraped metrics from %s are incomplete: "+
				"opened connections metric value is missing",
			target,
		)
		return nil
	}
	if !closedConnectionsMetricScraped {
		glog.Errorf(
			"Prometheus-scraped metrics from %s are incomplete: "+
				"closed connections metric value is missing",
			target,
		)
		return nil
	}

	return &pcs
}

// extractPrometheusMetricFamilyValue extracts a value from the given
// metricFamily if it has a metric that matches the required labels.
// Prometheus metrics are organized as follow:
// - top level "metric families", with a name and a type
//   - each metricFamily has one or more metrics, each with:
//     - a set of labels
//     - a value
// so to extract a value from a metricFamily, we need to find the matching
// metric based on the labels, and then return its value
func extractPrometheusMetricFamilyValue(
	metricFamily io_prometheus_client.MetricFamily,
	requiredLabels map[string]string,
) (uint64, bool) {
	for _, metric := range metricFamily.GetMetric() {
		value, found := extractPrometheusMetricValue(
			*metric,
			metricFamily,
			requiredLabels,
		)
		if !found {
			continue
		}
		return value, true
	}

	glog.Errorf(
		"Prometheus metric %s matches but no value was extracted - "+
			"maybe because of labels mismatch?",
		metricFamily.GetName(),
	)
	return 0, false
}

func extractPrometheusMetricValue(
	metric io_prometheus_client.Metric,
	metricFamily io_prometheus_client.MetricFamily,
	requiredLabels map[string]string,
) (uint64, bool) {
	for k, v := range requiredLabels {
		var found bool
		for _, label := range metric.GetLabel() {
			if label.GetName() == k && label.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			// this metric didn't matches, but maybe another one will
			return 0, false
		}
	}

	var metricValue float64
	switch metricFamily.GetType() {
	case io_prometheus_client.MetricType_COUNTER:
		counter := metric.GetCounter()
		if counter == nil {
			glog.Errorf(
				"Prometheus metric %s is registered as a counter metric, "+
					" but has no counter value",
				metricFamily.GetName(),
			)
			return 0, false
		}
		metricValue = counter.GetValue()
	case io_prometheus_client.MetricType_GAUGE:
		gauge := metric.GetGauge()
		if gauge == nil {
			glog.Errorf(
				"Prometheus metric %s is registered as a gauge metric, "+
					" but has no gauge value",
				metricFamily.GetName(),
			)
			return 0, false
		}
		metricValue = gauge.GetValue()
	case io_prometheus_client.MetricType_UNTYPED:
		untyped := metric.GetUntyped()
		if untyped != nil {
			glog.Errorf(
				"Prometheus metric %s is registered as an untyped metric, "+
					" but has no untyped value",
				metricFamily.GetName(),
			)
			return 0, false
		}
		metricValue = untyped.GetValue()
	default:
		glog.Errorf(
			"Prometheus metric %s has an unsupported type %s",
			metricFamily.GetName(),
			metricFamily.GetType().String(),
		)
		return 0, false
	}

	return uint64(metricValue), true
}
