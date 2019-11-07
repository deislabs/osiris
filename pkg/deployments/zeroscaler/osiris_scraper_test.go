package zeroscaler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestGetMetricsPort(t *testing.T) {
	tests := []struct {
		name           string
		pod            *corev1.Pod
		expectedValue  int32
		expectedStatus bool
	}{
		{
			name: "multiple containers & ports",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "some-container",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
								},
							},
						},
						{
							Name: proxyContainerName,
							Ports: []corev1.ContainerPort{
								{
									Name:          "some-port",
									ContainerPort: 8000,
								},
								{
									Name:          proxyPortName,
									ContainerPort: 5000,
								},
							},
						},
					},
				},
			},
			expectedValue:  5000,
			expectedStatus: true,
		},
		{
			name: "no proxy container",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "some-container",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
								},
							},
						},
					},
				},
			},
			expectedValue:  0,
			expectedStatus: false,
		},
	}

	scraper := newOsirisScraper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualValue, actualStatus := scraper.getMetricsPort(test.pod)

			assert.Equal(t, test.expectedValue, actualValue)
			assert.Equal(t, test.expectedStatus, actualStatus)
		})
	}
}
