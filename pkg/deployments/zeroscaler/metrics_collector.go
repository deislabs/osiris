package zeroscaler

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/deislabs/osiris/pkg/metrics"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8s_types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	proxyContainerName = "osiris-proxy"
	proxyPortName      = "osiris-metrics"
)

type metricsCollector struct {
	kubeClient           kubernetes.Interface
	deploymentName       string
	deploymentNamespace  string
	selector             labels.Selector
	metricsCheckInterval time.Duration
	podsInformer         cache.SharedIndexInformer
	currentAppPods       map[string]*corev1.Pod
	allAppPodStats       map[string]*podStats
	appPodsLock          sync.Mutex
	httpClient           *http.Client
	cancelFunc           func()
}

func newMetricsCollector(
	kubeClient kubernetes.Interface,
	deploymentName string,
	deploymentNamespace string,
	selector labels.Selector,
	metricsCheckInterval time.Duration,
) *metricsCollector {
	m := &metricsCollector{
		kubeClient:           kubeClient,
		deploymentName:       deploymentName,
		deploymentNamespace:  deploymentNamespace,
		selector:             selector,
		metricsCheckInterval: metricsCheckInterval,
		podsInformer: k8s.PodsIndexInformer(
			kubeClient,
			deploymentNamespace,
			nil,
			selector,
		),
		currentAppPods: map[string]*corev1.Pod{},
		allAppPodStats: map[string]*podStats{},
		// A very aggressive timeout. When collecting metrics, we want to do it very
		// quickly to minimize the possibility that some pods we've checked on have
		// served requests while we've been checking on OTHER pods.
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
	m.podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: m.syncAppPod,
		UpdateFunc: func(_, newObj interface{}) {
			m.syncAppPod(newObj)
		},
		DeleteFunc: m.syncDeletedAppPod,
	})
	return m
}

func (m *metricsCollector) run(ctx context.Context) {
	ctx, m.cancelFunc = context.WithCancel(ctx)
	defer m.cancelFunc()
	go func() {
		<-ctx.Done()
		glog.Infof(
			"Stopping metrics collection for deployment %s in namespace %s",
			m.deploymentName,
			m.deploymentNamespace,
		)
	}()
	glog.Infof(
		"Starting metrics collection for deployment %s in namespace %s",
		m.deploymentName,
		m.deploymentNamespace,
	)
	go m.podsInformer.Run(ctx.Done())
	// When this exits, the cancel func will stop the informer
	m.collectMetrics(ctx)
}

func (m *metricsCollector) stop() {
	m.cancelFunc()
}

func (m *metricsCollector) syncAppPod(obj interface{}) {
	m.appPodsLock.Lock()
	defer m.appPodsLock.Unlock()
	pod := obj.(*corev1.Pod)
	m.currentAppPods[pod.Name] = pod
	if _, ok := m.allAppPodStats[pod.Name]; !ok {
		m.allAppPodStats[pod.Name] = &podStats{}
	}
}

func (m *metricsCollector) syncDeletedAppPod(obj interface{}) {
	m.appPodsLock.Lock()
	defer m.appPodsLock.Unlock()
	pod := obj.(*corev1.Pod)
	delete(m.currentAppPods, pod.Name)
	now := time.Now()
	if ps, ok := m.allAppPodStats[pod.Name]; ok {
		ps.podDeletedTime = &now
	}
}

func (m *metricsCollector) collectMetrics(ctx context.Context) {
	ticker := time.NewTicker(m.metricsCheckInterval)
	defer ticker.Stop()
	var periodStartTime, periodEndTime *time.Time
	for {
		select {
		case <-ticker.C:
			periodStartTime = periodEndTime
			now := time.Now()
			periodEndTime = &now
			// Wrap in a function so we can more easily defer some cleanup that has
			// to be executed regardless of which condition causes us to continue to
			// the next iteration of the loop.
			func() {
				m.appPodsLock.Lock()
				defer m.appPodsLock.Unlock()
				// An aggressively small timeout. We make the decision fast or not at
				// all.
				timer := time.NewTimer(3 * time.Second)
				defer timer.Stop()
				var timedOut bool
				// Get metrics for all of the deployment's CURRENT pods.
				var scrapeWG sync.WaitGroup
				for _, pod := range m.currentAppPods {
					podMetricsPort, ok := getMetricsPort(pod)
					if !ok {
						continue
					}
					url := fmt.Sprintf(
						"http://%s:%d/metrics",
						pod.Status.PodIP,
						podMetricsPort,
					)
					scrapeWG.Add(1)
					go func(podName string) {
						defer scrapeWG.Done()
						// Get the results
						pcs, ok := m.scrape(url)
						if ok {
							ps := m.allAppPodStats[podName]
							ps.prevStatTime = ps.recentStatTime
							ps.prevStats = ps.recentStats
							ps.recentStatTime = periodEndTime
							ps.recentStats = &pcs
						}
					}(pod.Name)
				}
				// Wait until we're done checking all pods.
				scrapeWG.Wait()
				// If this is our first check, we're done because we will have no
				// previous stats to compare recent stats to.
				if periodStartTime == nil {
					return
				}
				// Now iterate over stats for ALL of the deployment's pods-- this may
				// include pods that died since the last check-- their stats should
				// still count, but since we won't have stats for those, we'll have to
				// err on the side of caution and assume activity in such cases.
				var foundActivity, assumedActivity bool
				for podName, ps := range m.allAppPodStats {
					if ps.podDeletedTime != nil &&
						ps.podDeletedTime.Before(*periodStartTime) {
						// This pod was deleted before the period we'red concerned with, so
						// stats from this pod are not relevant anymore or ever again.
						delete(m.allAppPodStats, podName)
						continue
					}
					// If we already assumed some activity or found some, fast forward to
					// the next pod. We don't simply break out of the whole loop because
					// the logic above removes stats for pods that are no longer relevant
					// and we still want that to happen promptly.
					if assumedActivity || foundActivity {
						continue
					}
					// nolint: lll
					if (ps.prevStatTime == nil || ps.recentStatTime == nil) || // We cannot make meaningful comparisons for this pod
						ps.recentStatTime.Before(*periodEndTime) || // We don't have up-to-date stats for this pod
						ps.recentStats.ProxyID != ps.prevStats.ProxyID { // The pod's metrics-collecting proxy sidecar died and was replaced
						assumedActivity = true
						continue
					}
					// nolint: lll
					if ps.recentStats.ConnectionsOpened > ps.prevStats.ConnectionsOpened || // New connections have been opened
						ps.recentStats.ConnectionsClosed > ps.prevStats.ConnectionsClosed || // Opened connections were closed
						ps.recentStats.ConnectionsOpened > ps.recentStats.ConnectionsClosed { // Some connections remain open
						foundActivity = true
						continue
					}
				}
				select {
				case <-timer.C:
					timedOut = true
				default:
				}
				if !(timedOut || foundActivity || assumedActivity) {
					m.scaleToZero()
				}
			}()
		case <-ctx.Done():
			return
		}
	}
}

func getMetricsPort(pod *corev1.Pod) (int32, bool) {
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

func (m *metricsCollector) scrape(
	target string,
) (metrics.ProxyConnectionStats, bool) {
	pcs := metrics.ProxyConnectionStats{}
	// Requests made with this client time out after 2 seconds
	resp, err := m.httpClient.Get(target)
	if err != nil {
		glog.Errorf("Error requesting metrics from %s: %s", target, err)
		return pcs, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		glog.Errorf(
			"Received unexpected HTTP response code %d when requesting metrics "+
				"from %s",
			resp.StatusCode,
			target,
		)
		return pcs, false
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf(
			"Error reading metrics request response from %s: %s",
			target,
			err,
		)
		return pcs, false
	}
	if err := json.Unmarshal(bodyBytes, &pcs); err != nil {
		glog.Errorf(
			"Error umarshaling metrics request response from %s: %s",
			target,
			err,
		)
		return pcs, false
	}
	return pcs, true
}

func (m *metricsCollector) scaleToZero() {
	glog.Infof(
		"Scale to zero starting for deployment %s in namespace %s",
		m.deploymentName,
		m.deploymentNamespace,
	)

	patches := []k8s.PatchOperation{{
		Op:    "replace",
		Path:  "/spec/replicas",
		Value: 0,
	}}
	patchesBytes, _ := json.Marshal(patches)
	if _, err := m.kubeClient.AppsV1().Deployments(m.deploymentNamespace).Patch(
		m.deploymentName,
		k8s_types.JSONPatchType,
		patchesBytes,
	); err != nil {
		glog.Errorf(
			"Error scaling deployment %s in namespace %s to zero: %s",
			m.deploymentName,
			m.deploymentNamespace,
			err,
		)
		return
	}

	glog.Infof(
		"Scaled deployment %s in namespace %s to zero",
		m.deploymentName,
		m.deploymentNamespace,
	)
}
