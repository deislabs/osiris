package injector

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	proxyInitContainerName = "osiris-proxy-init"
	proxyContainerName     = "osiris-proxy"
	proxyPortName          = "osiris-metrics"
)

func (i *injector) getPodPatchOperations(
	ar *v1beta1.AdmissionReview,
) ([]kubernetes.PatchOperation, error) {
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		glog.Errorf("Could not unmarshal raw object: %v", err)
		return nil, err
	}

	glog.Infof(
		"AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v "+
			"patchOperation=%v UserInfo=%v",
		req.Kind,
		req.Namespace,
		req.Name,
		pod.Name,
		req.UID,
		req.Operation,
		req.UserInfo,
	)

	if !kubernetes.ResourceIsOsirisEnabled(pod.Annotations) ||
		(podContainsProxyInitContainer(&pod) && podContainsProxyContainer(&pod)) {
		return nil, nil
	}

	// Pick an available port that will proxy each application port
	appPorts := getAppPorts(&pod)  // Ports exposed by existing containers
	usedPorts := getAppPorts(&pod) // Keep tracks of ALL used ports as we go
	portMappingStrs := []string{}
	for appPort := range appPorts {
		proxyPort := getNextAvailablePort(usedPorts)
		portMappingStrs = append(
			portMappingStrs,
			fmt.Sprintf("%d:%d", proxyPort, appPort),
		)
	}
	// This is the configuration string for the PORT_MAPPINGS env var used by both
	// the proxy sidecar and proxy init container
	portMappingStr := strings.Join(portMappingStrs, ",")

	patchOps := []kubernetes.PatchOperation{}

	if !podContainsProxyInitContainer(&pod) {

		proxyInitContainer := corev1.Container{
			Name:            proxyInitContainerName,
			Image:           i.config.ProxyImage,
			ImagePullPolicy: corev1.PullPolicy(i.config.ProxyImagePullPolicy),
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{"NET_ADMIN"},
				},
			},
			Command: []string{"/osiris/bin/osiris-proxy-iptables.sh"},
			Env: []corev1.EnvVar{
				{
					Name:  "PORT_MAPPINGS",
					Value: portMappingStr,
				},
			},
		}

		var path string
		var value interface{}
		if len(pod.Spec.InitContainers) == 0 {
			path = "/spec/initContainers"
			value = []corev1.Container{proxyInitContainer}
		} else {
			path = "/spec/initContainers/-"
			value = proxyInitContainer
		}

		patchOps = append(
			patchOps,
			kubernetes.PatchOperation{
				Op:    "add",
				Path:  path,
				Value: value,
			},
		)

	}

	if !podContainsProxyContainer(&pod) {

		// Pick one more available port for serving the proxy's own metrics and
		// health checks
		metricsAndHealthPort := getNextAvailablePort(usedPorts)

		proxyContainer := corev1.Container{
			Name:            proxyContainerName,
			Image:           i.config.ProxyImage,
			ImagePullPolicy: corev1.PullPolicy(i.config.ProxyImagePullPolicy),
			SecurityContext: &corev1.SecurityContext{
				RunAsUser: func() *int64 { var ret int64 = 1000; return &ret }(),
			},
			Command: []string{"/osiris/bin/osiris"},
			Args:    []string{"--logtostderr=true", "proxy"},
			Env: []corev1.EnvVar{
				{
					Name:  "PORT_MAPPINGS",
					Value: portMappingStr,
				},
				{
					Name:  "METRICS_AND_HEALTH_PORT",
					Value: fmt.Sprintf("%d", metricsAndHealthPort),
				},
				{
					Name:  "IGNORED_PATHS",
					Value: pod.Annotations[kubernetes.IgnoredPathsAnnotationName],
				},
			},
			Ports: []corev1.ContainerPort{
				{
					Name:          proxyPortName,
					ContainerPort: metricsAndHealthPort,
				},
			},
			LivenessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(int(metricsAndHealthPort)),
						Path: "/healthz",
					},
				},
			},
			ReadinessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(int(metricsAndHealthPort)),
						Path: "/healthz",
					},
				},
			},
		}

		var path string
		var value interface{}
		if len(pod.Spec.Containers) == 0 {
			path = "/spec/containers"
			value = []corev1.Container{proxyContainer}
		} else {
			path = "/spec/containers/-"
			value = proxyContainer
		}

		patchOps = append(
			patchOps,
			kubernetes.PatchOperation{
				Op:    "add",
				Path:  path,
				Value: value,
			},
		)

	}

	return patchOps, nil
}

func podContainsProxyInitContainer(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.InitContainers {
		if c.Name == proxyInitContainerName {
			return true
		}
	}
	return false
}

func podContainsProxyContainer(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == proxyContainerName {
			return true
		}
	}
	return false
}

func getAppPorts(pod *corev1.Pod) map[int32]struct{} {
	appPorts := map[int32]struct{}{}
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			appPorts[p.ContainerPort] = struct{}{}
		}
	}
	return appPorts
}

func getNextAvailablePort(usedPorts map[int32]struct{}) int32 {
	var port int32 = 5000
	for {
		if _, ok := usedPorts[port]; !ok {
			usedPorts[port] = struct{}{}
			return port
		}
		port++
	}
}
