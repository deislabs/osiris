package zeroscaler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/labels"
)

func TestShouldUpdateCollector(t *testing.T) {
	testcases := []struct {
		name                    string
		collector               *metricsCollector
		newSelector             labels.Selector
		newMetricsCheckInterval time.Duration
		expectedResult          bool
	}{
		{
			name: "same selector and metricsCheckInterval",
			collector: &metricsCollector{
				selector:             labels.Everything(),
				metricsCheckInterval: 5 * time.Second,
			},
			newSelector:             labels.Everything(),
			newMetricsCheckInterval: 5 * time.Second,
			expectedResult:          false,
		},
		{
			name: "same selector but different metricsCheckInterval",
			collector: &metricsCollector{
				selector:             labels.Everything(),
				metricsCheckInterval: 5 * time.Second,
			},
			newSelector:             labels.Everything(),
			newMetricsCheckInterval: 10 * time.Second,
			expectedResult:          true,
		},
		{
			name: "different selector but same metricsCheckInterval",
			collector: &metricsCollector{
				selector:             labels.Everything(),
				metricsCheckInterval: 5 * time.Second,
			},
			newSelector:             labels.Nothing(),
			newMetricsCheckInterval: 5 * time.Second,
			expectedResult:          true,
		},
		{
			name: "different selector and metricsCheckInterval",
			collector: &metricsCollector{
				selector:             labels.Everything(),
				metricsCheckInterval: 5 * time.Second,
			},
			newSelector:             labels.Nothing(),
			newMetricsCheckInterval: 10 * time.Second,
			expectedResult:          true,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			actual := shouldUpdateCollector(
				test.collector,
				test.newSelector,
				test.newMetricsCheckInterval,
			)

			assert.Equal(t, test.expectedResult, actual)
		})
	}
}
