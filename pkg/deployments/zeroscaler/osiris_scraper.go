package zeroscaler

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/deislabs/osiris/pkg/metrics"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
)

const (
	osirisScraperName = "osiris"

	proxyContainerName = "osiris-proxy"
	proxyPortName      = "osiris-metrics"
)

// osirisScraper is a metrics scraper that scraps metrics from the osiris proxy.
// It expects the scraped pods to contains a proxy that returned its metrics at
// the /metrics path, on a port with a well-identified name.
// This is the default metrics scraper implementation.
type osirisScraper struct {
	httpClient *http.Client
}

func newOsirisScraper() *osirisScraper {
	return &osirisScraper{
		// A very aggressive timeout. When collecting metrics, we want to do it very
		// quickly to minimize the possibility that some pods we've checked on have
		// served requests while we've been checking on OTHER pods.
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

func (s *osirisScraper) Scrap(pod *corev1.Pod) *metrics.ProxyConnectionStats {
	podMetricsPort, found := s.getMetricsPort(pod)
	if !found {
		glog.Errorf("Pod %s has no proxy container", pod.Name)
		return nil
	}

	target := fmt.Sprintf(
		"http://%s:%d/metrics",
		pod.Status.PodIP,
		podMetricsPort,
	)
	resp, err := s.httpClient.Get(target)
	if err != nil {
		glog.Errorf("Error requesting metrics from %s: %s", target, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		glog.Errorf(
			"Received unexpected HTTP response code %d when requesting metrics "+
				"from %s",
			resp.StatusCode,
			target,
		)
		return nil
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf(
			"Error reading metrics request response from %s: %s",
			target,
			err,
		)
		return nil
	}

	var pcs metrics.ProxyConnectionStats
	if err := json.Unmarshal(bodyBytes, &pcs); err != nil {
		glog.Errorf(
			"Error umarshaling metrics request response from %s: %s",
			target,
			err,
		)
		return nil
	}
	return &pcs
}

func (s *osirisScraper) getMetricsPort(pod *corev1.Pod) (int32, bool) {
	for _, c := range pod.Spec.Containers {
		if c.Name == proxyContainerName && len(c.Ports) > 0 {
			for _, port := range c.Ports {
				if port.Name == proxyPortName {
					return port.ContainerPort, true
				}
			}
		}
	}
	return 0, false
}
