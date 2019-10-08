package zeroscaler

import (
	"context"
	"fmt"
	"reflect"
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
	cfg                  Config
	kubeClient           kubernetes.Interface
	deploymentsInformer  cache.SharedInformer
	statefulSetsInformer cache.SharedInformer
	collectors           map[string]*metricsCollector
	collectorsLock       sync.Mutex
	ctx                  context.Context
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
		statefulSetsInformer: k8s.StatefulSetsIndexInformer(
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
	z.statefulSetsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: z.syncStatefulSet,
		UpdateFunc: func(_, newObj interface{}) {
			z.syncStatefulSet(newObj)
		},
		DeleteFunc: z.syncDeletedStatefulSet,
	})
	return z
}

// Run causes the controller to collect metrics for Osiris-enabled
// deployments and statefulSets.
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
	go func() {
		z.statefulSetsInformer.Run(ctx.Done())
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
			z.ensureMetricsCollection(
				"Deployment",
				deployment.Namespace,
				deployment.Name,
				deployment.Spec.Selector,
			)
		} else {
			glog.Infof(
				"Osiris-enabled deployment %s in namespace %s is running zero "+
					"replicas OR more than the minimum number of replicas; ensuring "+
					"NO metrics collection",
				deployment.Name,
				deployment.Namespace,
			)
			z.ensureNoMetricsCollection(
				"Deployment",
				deployment.Namespace,
				deployment.Name,
			)
		}
	} else {
		glog.Infof(
			"Notified about new or updated non-Osiris-enabled deployment %s in "+
				"namespace %s; ensuring NO metrics collection",
			deployment.Name,
			deployment.Namespace,
		)
		z.ensureNoMetricsCollection(
			"Deployment",
			deployment.Namespace,
			deployment.Name,
		)
	}
}

func (z *zeroscaler) syncStatefulSet(obj interface{}) {
	statefulSet := obj.(*appsv1.StatefulSet)
	if k8s.ResourceIsOsirisEnabled(statefulSet.Annotations) {
		glog.Infof(
			"Notified about new or updated Osiris-enabled statefulSet %s in "+
				"namespace %s",
			statefulSet.Name,
			statefulSet.Namespace,
		)
		minReplicas := k8s.GetMinReplicas(statefulSet.Annotations, 1)
		if *statefulSet.Spec.Replicas > 0 &&
			statefulSet.Status.ReadyReplicas <= minReplicas {
			glog.Infof(
				"Osiris-enabled statefulSet %s in namespace %s is running the minimun "+
					"number of replicas or fewer; ensuring metrics collection",
				statefulSet.Name,
				statefulSet.Namespace,
			)
			z.ensureMetricsCollection(
				"StatefulSet",
				statefulSet.Namespace,
				statefulSet.Name,
				statefulSet.Spec.Selector,
			)
		} else {
			glog.Infof(
				"Osiris-enabled statefulSet %s in namespace %s is running zero "+
					"replicas OR more than the minimum number of replicas; ensuring "+
					"NO metrics collection",
				statefulSet.Name,
				statefulSet.Namespace,
			)
			z.ensureNoMetricsCollection(
				"StatefulSet",
				statefulSet.Namespace,
				statefulSet.Name,
			)
		}
	} else {
		glog.Infof(
			"Notified about new or updated non-Osiris-enabled statefulSet %s in "+
				"namespace %s; ensuring NO metrics collection",
			statefulSet.Name,
			statefulSet.Namespace,
		)
		z.ensureNoMetricsCollection(
			"StatefulSet",
			statefulSet.Namespace,
			statefulSet.Name,
		)
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
	z.ensureNoMetricsCollection(
		"Deployment",
		deployment.Namespace,
		deployment.Name,
	)
}

func (z *zeroscaler) syncDeletedStatefulSet(obj interface{}) {
	statefulSet := obj.(*appsv1.StatefulSet)
	glog.Infof(
		"Notified about deleted statefulSet %s in namespace %s; ensuring NO "+
			"metrics collection",
		statefulSet.Name,
		statefulSet.Namespace,
	)
	z.ensureNoMetricsCollection(
		"StatefulSet",
		statefulSet.Namespace,
		statefulSet.Name,
	)
}

func (z *zeroscaler) ensureMetricsCollection(kind, namespace, name string,
	labelSelector *metav1.LabelSelector) {
	z.collectorsLock.Lock()
	defer z.collectorsLock.Unlock()
	key := getKey(kind, namespace, name)
	selector := labels.SelectorFromSet(labelSelector.MatchLabels)
	if collector, ok := z.collectors[key]; !ok ||
		!reflect.DeepEqual(selector, collector.selector) {
		if ok {
			collector.stop()
		}
		glog.Infof(
			"Using new metrics collector for %s %s in namespace %s",
			kind,
			name,
			namespace,
		)
		collector := newMetricsCollector(
			z.kubeClient,
			kind,
			name,
			namespace,
			selector,
			time.Duration(z.cfg.MetricsCheckInterval)*time.Second,
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
		"Using existing metrics collector for %s %s in namespace %s",
		kind,
		name,
		namespace,
	)
}

func (z *zeroscaler) ensureNoMetricsCollection(kind, namespace, name string) {
	z.collectorsLock.Lock()
	defer z.collectorsLock.Unlock()
	key := getKey(kind, namespace, name)
	if collector, ok := z.collectors[key]; ok {
		collector.stop()
		delete(z.collectors, key)
	}
}

func getKey(kind, namespace, name string) string {
	return fmt.Sprintf("%s:%s/%s", kind, namespace, name)
}
