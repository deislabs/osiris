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
				"osiris.kubernetes.io/enabled": "1",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value true",
			annotations: map[string]string{
				"osiris.kubernetes.io/enabled": "true",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value on",
			annotations: map[string]string{
				"osiris.kubernetes.io/enabled": "on",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value y",
			annotations: map[string]string{
				"osiris.kubernetes.io/enabled": "y",
			},
			expectedResult: true,
		},
		{
			name: "map with osiris enabled entry and value yes",
			annotations: map[string]string{
				"osiris.kubernetes.io/enabled": "yes",
			},
			expectedResult: true,
		},
		{
			name: "map with no osiris enabled entry ",
			annotations: map[string]string{
				"osiris.kubernetes.io/notenabled": "yes",
			},
			expectedResult: false,
		},

		{
			name: "map with osiris enabled entry and invalid value",
			annotations: map[string]string{
				"osiris.kubernetes.io/enabled": "yee",
			},
			expectedResult: false,
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			actual := ResourceIsOsirisEnabled(test.annotations)
			if actual != test.expectedResult {
				t.Errorf(
					"expected GetMinReplicas to return %t, but got %t",
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
				"osiris.kubernetes.io/minReplicas": "3",
			},
			expectedResult: 3,
		},
		{
			name: "map with no min replicas entry",
			annotations: map[string]string{
				"osiris.kubernetes.io/notminReplicas": "3",
			},
			expectedResult: 1,
		},
		{
			name: "map with invalid min replicas entry",
			annotations: map[string]string{
				"osiris.kubernetes.io/minReplicas": "invalid",
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
