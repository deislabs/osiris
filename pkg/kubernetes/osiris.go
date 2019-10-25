package kubernetes

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	IgnoredPathsAnnotationName         = "osiris.deislabs.io/ignoredPaths"
	osirisEnabledAnnotationName        = "osiris.deislabs.io/enabled"
	injectProxyAnnotationName          = "osiris.deislabs.io/injectProxy"
	metricsCheckIntervalAnnotationName = "osiris.deislabs.io/metricsCheckInterval"
)

// ResourceIsOsirisEnabled checks the annotations to see if the
// kube resource is enabled for osiris or not.
func ResourceIsOsirisEnabled(annotations map[string]string) bool {
	return annotationBooleanValue(annotations, osirisEnabledAnnotationName)
}

// PodIsEligibleForProxyInjection checks the annotations to see if the
// pod is eligible for proxy injection or not.
func PodIsEligibleForProxyInjection(annotations map[string]string) bool {
	return annotationBooleanValue(annotations, injectProxyAnnotationName)
}

func annotationBooleanValue(annotations map[string]string, key string) bool {
	enabled, ok := annotations[key]
	if !ok {
		return false
	}
	switch strings.ToLower(enabled) {
	case "y", "yes", "true", "on", "1":
		return true
	default:
		return false
	}
}

// GetMinReplicas gets the minimum number of replicas required for scale up
// from the annotations. If it fails to do so, it returns the default value
// instead.
func GetMinReplicas(annotations map[string]string, defaultVal int32) int32 {
	val, ok := annotations["osiris.deislabs.io/minReplicas"]
	if !ok {
		return defaultVal
	}
	minReplicas, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return int32(minReplicas)
}

// GetMetricsCheckInterval gets the interval in which the zeroScaler would
// repeatedly track the pod http request metrics. The value is the number
// of seconds of the interval. If it fails to do so, it returns an error.
func GetMetricsCheckInterval(annotations map[string]string) (int, error) {
	if len(annotations) == 0 {
		return 0, nil
	}
	val, ok := annotations[metricsCheckIntervalAnnotationName]
	if !ok {
		return 0, nil
	}
	metricsCheckInterval, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid int value '%s' for '%s' annotation: %s",
			val, metricsCheckIntervalAnnotationName, err)
	}
	if metricsCheckInterval <= 0 {
		return 0, fmt.Errorf("metricsCheckInterval should be positive, "+
			"'%d' is not a valid value",
			metricsCheckInterval)
	}
	return metricsCheckInterval, nil
}
