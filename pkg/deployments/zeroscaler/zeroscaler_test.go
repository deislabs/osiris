package zeroscaler

import (
	"testing"
	"time"

	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestGetMetricsCheckInterval(t *testing.T) {
	testcases := []struct {
		name           string
		zeroScaler     *zeroscaler
		deployment     *appsv1.Deployment
		expectedResult time.Duration
	}{
		{
			name: "no specific annotation",
			zeroScaler: &zeroscaler{
				cfg: Config{
					MetricsCheckInterval: 150,
				},
			},
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expectedResult: 150 * time.Second,
		},
		{
			name: "custom valid annotation",
			zeroScaler: &zeroscaler{
				cfg: Config{
					MetricsCheckInterval: 150,
				},
			},
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCheckIntervalAnnotationName: "60",
					},
				},
			},
			expectedResult: 60 * time.Second,
		},
		{
			name: "custom invalid annotation value",
			zeroScaler: &zeroscaler{
				cfg: Config{
					MetricsCheckInterval: 150,
				},
			},
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCheckIntervalAnnotationName: "something",
					},
				},
			},
			expectedResult: 150 * time.Second,
		},
		{
			name: "custom negative annotation value",
			zeroScaler: &zeroscaler{
				cfg: Config{
					MetricsCheckInterval: 150,
				},
			},
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCheckIntervalAnnotationName: "-60",
					},
				},
			},
			expectedResult: 150 * time.Second,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			actual := test.zeroScaler.getMetricsCheckInterval(test.deployment)

			assert.Equal(t, test.expectedResult, actual)
		})
	}
}
