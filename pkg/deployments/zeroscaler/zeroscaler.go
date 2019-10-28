package zeroscaler

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/deislabs/osiris/pkg/healthz"
	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Zeroscaler interface {
	Run(ctx context.Context)
}

type zeroscaler struct {
	cfg                 Config
	kubeClient          kubernetes.Interface
	deploymentsInformer cache.SharedInformer
	collectors          map[string]*metricsCollector
	collectorsLock      sync.Mutex
	ctx                 context.Context
}

func NewZeroscaler(cfg Config, kubeClient kubernetes.Interface) Zeroscaler {
	z := &zeroscaler{
		cfg:        cfg,
		kubeClient: kubeClient,
		deploymentsInformer: k8s.DeploymentsIndexInformer(
			kubeClient,
			metav1.NamespaceAll,
			nil,
			nil,
		),
		collectors: map[string]*metricsCollector{},
	}
	z.deploymentsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: z.syncDeployment,
		UpdateFunc: func(_, newObj interface{}) {
			z.syncDeployment(newObj)
		},
		DeleteFunc: z.syncDeletedDeployment,
	})
	return z
}

// Run causes the controller to collect metrics for Osiris-enabled deployments.
func (z *zeroscaler) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	z.ctx = ctx
	go func() {
		<-ctx.Done()
		glog.Infof("Zeroscaler is shutting down")
	}()
	glog.Infof("Zeroscaler is started")
	go func() {
		z.deploymentsInformer.Run(ctx.Done())
		cancel()
	}()
	healthz.RunServer(ctx, 5000)
	cancel()
}

func (z *zeroscaler) syncDeployment(obj interface{}) {
	deployment := obj.(*appsv1.Deployment)
	if k8s.ResourceIsOsirisEnabled(deployment.Annotations) {
		glog.Infof(
			"Notified about new or updated Osiris-enabled deployment %s in "+
				"namespace %s",
			deployment.Name,
			deployment.Namespace,
		)
		minReplicas := k8s.GetMinReplicas(deployment.Annotations, 1)
		if *deployment.Spec.Replicas > 0 &&
			deployment.Status.AvailableReplicas <= minReplicas {
			glog.Infof(
				"Osiris-enabled deployment %s in namespace %s is running the minimun "+
					"number of replicas or fewer; ensuring metrics collection",
				deployment.Name,
				deployment.Namespace,
			)
			z.ensureMetricsCollection(deployment)
		} else {
			glog.Infof(
				"Osiris-enabled deployment %s in namespace %s is running zero "+
					"replicas OR more than the minimum number of replicas; ensuring "+
					"NO metrics collection",
				deployment.Name,
				deployment.Namespace,
			)
			z.ensureNoMetricsCollection(deployment)
		}
	} else {
		glog.Infof(
			"Notified about new or updated non-Osiris-enabled deployment %s in "+
				"namespace %s; ensuring NO metrics collection",
			deployment.Name,
			deployment.Namespace,
		)
		z.ensureNoMetricsCollection(deployment)
	}
}

func (z *zeroscaler) syncDeletedDeployment(obj interface{}) {
	deployment := obj.(*appsv1.Deployment)
	glog.Infof(
		"Notified about deleted deployment %s in namespace %s; ensuring NO "+
			"metrics collection",
		deployment.Name,
		deployment.Namespace,
	)
	z.ensureNoMetricsCollection(deployment)
}

func (z *zeroscaler) ensureMetricsCollection(deployment *appsv1.Deployment) {
	z.collectorsLock.Lock()
	defer z.collectorsLock.Unlock()
	key := getDeploymentKey(deployment)
	selector := labels.SelectorFromSet(deployment.Spec.Selector.MatchLabels)
	metricsCheckInterval := z.getMetricsCheckInterval(deployment)
	if collector, ok := z.collectors[key]; !ok ||
		shouldUpdateCollector(collector, selector, metricsCheckInterval) {
		if ok {
			collector.stop()
		}
		glog.Infof(
			"Using new metrics collector for deployment %s in namespace %s "+
				"with metrics check interval of %s",
			deployment.Name,
			deployment.Namespace,
			metricsCheckInterval.String(),
		)
		collector := newMetricsCollector(
			z.kubeClient,
			deployment.Name,
			deployment.Namespace,
			selector,
			metricsCheckInterval,
		)
		go func() {
			collector.run(z.ctx)
			// Once the collector has run to completion (scaled to zero) remove it
			// from the map
			z.collectorsLock.Lock()
			defer z.collectorsLock.Unlock()
			delete(z.collectors, key)
		}()
		z.collectors[key] = collector
		return
	}
	glog.Infof(
		"Using existing metrics collector for deployment %s in namespace %s",
		deployment.Name,
		deployment.Namespace,
	)
}

func (z *zeroscaler) ensureNoMetricsCollection(deployment *appsv1.Deployment) {
	z.collectorsLock.Lock()
	defer z.collectorsLock.Unlock()
	key := getDeploymentKey(deployment)
	if collector, ok := z.collectors[key]; ok {
		collector.stop()
		delete(z.collectors, key)
	}
}

func (z *zeroscaler) getMetricsCheckInterval(
	deployment *appsv1.Deployment,
) time.Duration {
	var (
		metricsCheckInterval int
		err                  error
	)
	if rawMetricsCheckInterval, ok :=
		deployment.Annotations[k8s.MetricsCheckIntervalAnnotationName]; ok {
		metricsCheckInterval, err = strconv.Atoi(rawMetricsCheckInterval)
		if err != nil {
			glog.Warningf(
				"There was an error getting custom metrics check interval value "+
					"in deployment %s, falling back to the default value of %d "+
					"seconds; error: %s",
				deployment.Name,
				z.cfg.MetricsCheckInterval,
				err,
			)
			metricsCheckInterval = z.cfg.MetricsCheckInterval
		}
	}
	if metricsCheckInterval <= 0 {
		glog.Warningf(
			"Invalid custom metrics check interval value %d in deployment %s,"+
				" falling back to the default value of %d seconds",
			metricsCheckInterval,
			deployment.Name,
			z.cfg.MetricsCheckInterval,
		)
		metricsCheckInterval = z.cfg.MetricsCheckInterval
	}
	return time.Duration(metricsCheckInterval) * time.Second
}

func shouldUpdateCollector(
	collector *metricsCollector,
	newSelector labels.Selector,
	newMetricsCheckInterval time.Duration,
) bool {
	if !reflect.DeepEqual(newSelector, collector.selector) {
		return true
	}
	if newMetricsCheckInterval != collector.metricsCheckInterval {
		return true
	}
	return false
}

func getDeploymentKey(deployment *appsv1.Deployment) string {
	return fmt.Sprintf("%s:%s", deployment.Namespace, deployment.Name)
}
