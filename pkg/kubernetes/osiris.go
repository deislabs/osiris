package kubernetes

import (
	"strconv"
	"strings"
)

const (
	IgnoredPathsAnnotationName = "osiris.deislabs.io/ignoredPaths"
)

// ResourceIsOsirisEnabled checks the annotations to see if the
// kube resource is enabled for osiris or not.
func ResourceIsOsirisEnabled(annotations map[string]string) bool {
	enabled, ok := annotations["osiris.deislabs.io/enabled"]
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

// GetIgnoredPaths gets the list of paths that should be ignored in the
// metrics, from the annotations.
func GetIgnoredPaths(annotations map[string]string) []string {
	if len(annotations) == 0 {
		return nil
	}
	val, ok := annotations[IgnoredPathsAnnotationName]
	if !ok {
		return nil
	}
	return strings.Split(val, ",")
}
