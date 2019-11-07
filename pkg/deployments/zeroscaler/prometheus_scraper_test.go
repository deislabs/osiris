package zeroscaler

import (
	"testing"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
)

func TestExtractPrometheusMetricFamilyValue(t *testing.T) {
	var (
		metricName       = "some-name"
		metricLabelKey   = "some-label-key"
		metricLabelValue = "some-label-value"
		metricValue      = float64(42)
	)

	tests := []struct {
		name           string
		metricFamily   io_prometheus_client.MetricFamily
		requiredLabels map[string]string
		expectedValue  uint64
		expectedStatus bool
	}{
		{
			name: "simple gauge with no labels",
			metricFamily: io_prometheus_client.MetricFamily{
				Name: &metricName,
				Type: io_prometheus_client.MetricType_GAUGE.Enum(),
				Metric: []*io_prometheus_client.Metric{
					{Gauge: &io_prometheus_client.Gauge{Value: &metricValue}},
				},
			},
			expectedValue:  42,
			expectedStatus: true,
		},
		{
			name: "counter with matching labels",
			metricFamily: io_prometheus_client.MetricFamily{
				Name: &metricName,
				Type: io_prometheus_client.MetricType_COUNTER.Enum(),
				Metric: []*io_prometheus_client.Metric{
					{
						Label: []*io_prometheus_client.LabelPair{
							{Name: &metricLabelKey, Value: &metricLabelValue},
						},
						Counter: &io_prometheus_client.Counter{Value: &metricValue},
					},
				},
			},
			requiredLabels: map[string]string{metricLabelKey: metricLabelValue},
			expectedValue:  42,
			expectedStatus: true,
		},
		{
			name: "untyped metric with unmatching labels",
			metricFamily: io_prometheus_client.MetricFamily{
				Name: &metricName,
				Type: io_prometheus_client.MetricType_UNTYPED.Enum(),
				Metric: []*io_prometheus_client.Metric{
					{
						Label: []*io_prometheus_client.LabelPair{
							{Name: &metricLabelKey, Value: &metricLabelValue},
						},
						Untyped: &io_prometheus_client.Untyped{Value: &metricValue},
					},
				},
			},
			requiredLabels: map[string]string{"whatever": "something"},
			expectedValue:  0,
			expectedStatus: false,
		},
		{
			name: "unsupported metric type",
			metricFamily: io_prometheus_client.MetricFamily{
				Name: &metricName,
				Type: io_prometheus_client.MetricType_SUMMARY.Enum(),
				Metric: []*io_prometheus_client.Metric{
					{Summary: &io_prometheus_client.Summary{}},
				},
			},
			expectedValue:  0,
			expectedStatus: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualValue, actualStatus := extractPrometheusMetricFamilyValue(
				test.metricFamily,
				test.requiredLabels,
			)

			assert.Equal(t, test.expectedValue, actualValue)
			assert.Equal(t, test.expectedStatus, actualStatus)
		})
	}
}
