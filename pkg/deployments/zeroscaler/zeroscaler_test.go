package zeroscaler

import (
	"testing"

	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetMetricsScraperConfig(t *testing.T) {
	tests := []struct {
		name           string
		deployment     *appsv1.Deployment
		expectedResult metricsScraperConfig
	}{
		{
			name:       "use osiris scraper as the default value",
			deployment: &appsv1.Deployment{},
			expectedResult: metricsScraperConfig{
				ScraperName: osirisScraperName,
			},
		},
		{
			name: "custom annotation for osiris",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCollectorAnnotationName: `
						{
							"type": "osiris"
						}
						`,
					},
				},
			},
			expectedResult: metricsScraperConfig{
				ScraperName: osirisScraperName,
			},
		},
		{
			name: "custom annotation for prometheus",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCollectorAnnotationName: `
						{
							"type": "prometheus"
						}
						`,
					},
				},
			},
			expectedResult: metricsScraperConfig{
				ScraperName: prometheusScraperName,
			},
		},
		{
			name: "custom annotation for prometheus with specific impl",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCollectorAnnotationName: `
						{
							"type": "prometheus",
							"implementation": {
								"port": 8080,
								"path": "/metrics",
								"openedConnectionsMetricName": "connections",
								"openedConnectionsMetricLabels": {
									"type": "opened"
								},
								"closedConnectionsMetricName": "connections",
								"closedConnectionsMetricLabels": {
									"type": "closed"
								}
							}
						}
						`,
					},
				},
			},
			expectedResult: metricsScraperConfig{
				ScraperName: prometheusScraperName,
				Implementation: []byte(`
				{
					"port": 8080,
					"path": "/metrics",
					"openedConnectionsMetricName": "connections",
					"openedConnectionsMetricLabels": {
						"type": "opened"
					},
					"closedConnectionsMetricName": "connections",
					"closedConnectionsMetricLabels": {
						"type": "closed"
					}
				}
				`),
			},
		},
		{
			name: "non-json content should fallback to osiris",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						k8s.MetricsCollectorAnnotationName: "some non-json content",
					},
				},
			},
			expectedResult: metricsScraperConfig{
				ScraperName: osirisScraperName,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := getMetricsScraperConfig(test.deployment)

			assert.Equal(t, test.expectedResult.ScraperName, actual.ScraperName)
			if len(test.expectedResult.Implementation) > 0 {
				assert.JSONEq(
					t,
					string(test.expectedResult.Implementation),
					string(actual.Implementation),
				)
			}
		})
	}
}
