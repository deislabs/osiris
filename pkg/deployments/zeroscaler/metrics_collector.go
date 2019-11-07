package zeroscaler

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8s_types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type metricsCollectorConfig struct {
	deploymentName       string
	deploymentNamespace  string
	selector             labels.Selector
	metricsCheckInterval time.Duration
	scraperConfig        metricsScraperConfig
}

type metricsCollector struct {
	config         metricsCollectorConfig
	scraper        metricsScraper
	kubeClient     kubernetes.Interface
	podsInformer   cache.SharedIndexInformer
	currentAppPods map[string]*corev1.Pod
	allAppPodStats map[string]*podStats
	appPodsLock    sync.Mutex
	cancelFunc     func()
}

func newMetricsCollector(
	kubeClient kubernetes.Interface,
	config metricsCollectorConfig,
) (*metricsCollector, error) {
	s, err := newMetricsScraper(config.scraperConfig)
	if err != nil {
		return nil, err
	}
	m := &metricsCollector{
		config:     config,
		scraper:    s,
		kubeClient: kubeClient,
		podsInformer: k8s.PodsIndexInformer(
			kubeClient,
			config.deploymentNamespace,
			nil,
			config.selector,
		),
		currentAppPods: map[string]*corev1.Pod{},
		allAppPodStats: map[string]*podStats{},
	}
	m.podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: m.syncAppPod,
		UpdateFunc: func(_, newObj interface{}) {
			m.syncAppPod(newObj)
		},
		DeleteFunc: m.syncDeletedAppPod,
	})
	return m, nil
}

func (m *metricsCollector) run(ctx context.Context) {
	ctx, m.cancelFunc = context.WithCancel(ctx)
	defer m.cancelFunc()
	go func() {
		<-ctx.Done()
		glog.Infof(
			"Stopping metrics collection for deployment %s in namespace %s",
			m.config.deploymentName,
			m.config.deploymentNamespace,
		)
	}()
	glog.Infof(
		"Starting metrics collection for deployment %s in namespace %s",
		m.config.deploymentName,
		m.config.deploymentNamespace,
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
	ticker := time.NewTicker(m.config.metricsCheckInterval)
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
					scrapeWG.Add(1)
					go func(pod *corev1.Pod) {
						defer scrapeWG.Done()
						// Get the results
						pcs := m.scraper.Scrap(pod)
						if pcs != nil {
							ps := m.allAppPodStats[pod.Name]
							ps.prevStatTime = ps.recentStatTime
							ps.prevStats = ps.recentStats
							ps.recentStatTime = periodEndTime
							ps.recentStats = pcs
						}
					}(pod)
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

func (m *metricsCollector) scaleToZero() {
	glog.Infof(
		"Scale to zero starting for deployment %s in namespace %s",
		m.config.deploymentName,
		m.config.deploymentNamespace,
	)

	patches := []k8s.PatchOperation{{
		Op:    "replace",
		Path:  "/spec/replicas",
		Value: 0,
	}}
	patchesBytes, _ := json.Marshal(patches)
	if _, err := m.kubeClient.AppsV1().Deployments(
		m.config.deploymentNamespace,
	).Patch(
		m.config.deploymentName,
		k8s_types.JSONPatchType,
		patchesBytes,
	); err != nil {
		glog.Errorf(
			"Error scaling deployment %s in namespace %s to zero: %s",
			m.config.deploymentName,
			m.config.deploymentNamespace,
			err,
		)
		return
	}

	glog.Infof(
		"Scaled deployment %s in namespace %s to zero",
		m.config.deploymentName,
		m.config.deploymentNamespace,
	)
}
