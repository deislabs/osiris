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
	appPods              map[string]*corev1.Pod
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
		appPods: map[string]*corev1.Pod{},
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
	m.appPods[pod.Name] = pod
}

func (m *metricsCollector) syncDeletedAppPod(obj interface{}) {
	m.appPodsLock.Lock()
	defer m.appPodsLock.Unlock()
	pod := obj.(*corev1.Pod)
	delete(m.appPods, pod.Name)
}

func (m *metricsCollector) collectMetrics(ctx context.Context) {
	requestCountsByProxy := map[string]uint64{}
	var lastTotalRequestCount uint64
	ticker := time.NewTicker(m.metricsCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.appPodsLock.Lock()
			var mustNotDecide bool
			var scrapeWG sync.WaitGroup
			// An aggressively small timeout. We make the decision fast or not at
			// all.
			timer := time.NewTimer(3 * time.Second)
			for _, pod := range m.appPods {
				podMetricsPort, ok := getMetricsPort(pod)
				if !ok {
					mustNotDecide = true
					continue
				}
				url := fmt.Sprintf(
					"http://%s:%d/metrics",
					pod.Status.PodIP,
					podMetricsPort,
				)
				scrapeWG.Add(1)
				go func() {
					defer scrapeWG.Done()
					// Get the results
					prc, ok := m.scrape(url)
					if !ok {
						mustNotDecide = true
					}
					requestCountsByProxy[prc.ProxyID] = prc.RequestCount
				}()
			}
			m.appPodsLock.Unlock()
			scrapeWG.Wait()
			var totalRequestCount uint64
			for _, requestCount := range requestCountsByProxy {
				totalRequestCount += requestCount
			}
			select {
			case <-timer.C:
				mustNotDecide = true
			case <-ctx.Done():
				return
			default:
			}
			timer.Stop()
			if !mustNotDecide && totalRequestCount == lastTotalRequestCount {
				m.scaleToZero()
			}
			lastTotalRequestCount = totalRequestCount
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
) (metrics.ProxyRequestCount, bool) {
	prc := metrics.ProxyRequestCount{}
	// Requests made with this client time out after 2 seconds
	resp, err := m.httpClient.Get(target)
	if err != nil {
		glog.Errorf("Error requesting metrics from %s: %s", target, err)
		return prc, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		glog.Errorf(
			"Received unexpected HTTP response code %d when requesting metrics "+
				"from %s",
			resp.StatusCode,
			target,
		)
		return prc, false
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf(
			"Error reading metrics request response from %s: %s",
			target,
			err,
		)
		return prc, false
	}
	if err := json.Unmarshal(bodyBytes, &prc); err != nil {
		glog.Errorf(
			"Error umarshaling metrics request response from %s: %s",
			target,
			err,
		)
		return prc, false
	}
	return prc, true
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
