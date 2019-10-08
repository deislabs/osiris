package kubernetes

import "testing"

func TestResourceIsOsirisEnabled(t *testing.T) {
	testcases := []struct {
		name           string
		annotations    map[string]string
		expectedResult bool
	}{
		{
			name: "map with osiris enabled entry and value 1",
			annotations: map[string]string{
				"osiris.deislabs.io/enabled": "1",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value true",
			annotations: map[string]string{
				"osiris.deislabs.io/enabled": "true",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value on",
			annotations: map[string]string{
				"osiris.deislabs.io/enabled": "on",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value y",
			annotations: map[string]string{
				"osiris.deislabs.io/enabled": "y",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value yes",
			annotations: map[string]string{
				"osiris.deislabs.io/enabled": "yes",
			},
			expectedResult: true,
		},
		{
			name: "map with no osiris enabled entry ",
			annotations: map[string]string{
				"osiris.deislabs.io/notenabled": "yes",
			},
			expectedResult: false,
		},

		{
			name: "map with osiris enabled entry and invalid value",
			annotations: map[string]string{
				"osiris.deislabs.io/enabled": "yee",
			},
			expectedResult: false,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			actual := ResourceIsOsirisEnabled(test.annotations)
			if actual != test.expectedResult {
				t.Errorf(
					"expected ResourceIsOsirisEnabled to return %t, but got %t",
					test.expectedResult, actual)
			}
		})
	}

}

func TestGetMinReplicas(t *testing.T) {
	testcases := []struct {
		name           string
		annotations    map[string]string
		expectedResult int32
	}{
		{
			name: "map with min replicas entry",
			annotations: map[string]string{
				"osiris.deislabs.io/minReplicas": "3",
			},
			expectedResult: 3,
		},
		{
			name: "map with no min replicas entry",
			annotations: map[string]string{
				"osiris.deislabs.io/notminReplicas": "3",
			},
			expectedResult: 1,
		},
		{
			name: "map with invalid min replicas entry",
			annotations: map[string]string{
				"osiris.deislabs.io/minReplicas": "invalid",
			},
			expectedResult: 1,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			actual := GetMinReplicas(test.annotations, 1)
			if actual != test.expectedResult {
				t.Errorf(
					"expected GetMinReplicas to return %d, but got %d",
					test.expectedResult, actual)
			}
		})
	}
}

func TestGetMetricsCheckInterval(t *testing.T) {
	testcases := []struct {
		name           string
		annotations    map[string]string
		expectedResult int
		expectedError  bool
	}{
		{
			name:           "nil map",
			annotations:    nil,
			expectedResult: 0,
			expectedError:  false,
		},
		{
			name:           "empty map",
			annotations:    map[string]string{},
			expectedResult: 0,
			expectedError:  false,
		},
		{
			name: "map with no metrics check interval entry",
			annotations: map[string]string{
				"whatever": "60",
			},
			expectedResult: 0,
			expectedError:  false,
		},
		{
			name: "map with invalid metrics check interval entry",
			annotations: map[string]string{
				"osiris.deislabs.io/metricsCheckInterval": "invalid",
			},
			expectedResult: 0,
			expectedError:  true,
		},
		{
			name: "map with negative metrics check interval entry",
			annotations: map[string]string{
				"osiris.deislabs.io/metricsCheckInterval": "-1",
			},
			expectedResult: 0,
			expectedError:  true,
		},
		{
			name: "map with zero metrics check interval entry",
			annotations: map[string]string{
				"osiris.deislabs.io/metricsCheckInterval": "0",
			},
			expectedResult: 0,
			expectedError:  true,
		},
		{
			name: "map with valid metrics check interval entry",
			annotations: map[string]string{
				"osiris.deislabs.io/metricsCheckInterval": "60",
			},
			expectedResult: 60,
			expectedError:  false,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			actual, err := GetMetricsCheckInterval(test.annotations)
			if err != nil {
				if !test.expectedError {
					t.Errorf(
						"expected GetMetricsCheckInterval to return %d, but got error %v",
						test.expectedResult, err)
				}
			} else {
				if test.expectedError {
					t.Error(
						"expected GetMetricsCheckInterval to return an error, but got none")
				}
			}

			if actual != test.expectedResult {
				t.Errorf(
					"expected GetMetricsCheckInterval to return %d, but got %d",
					test.expectedResult, actual)
			}
		})
	}
}
