package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/deislabs/osiris/pkg/healthz"
	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Controller is an interface for a component that can take over management of
// endpoints resources corresponding to selector-less, Osiris-enabled services
type Controller interface {
	// Run causes the controller to manage endpoints resources corresponding to
	// selector-less, Osiris-enabled services. This function will not return
	// until the context it has been passed expires or is canceled.
	Run(ctx context.Context)
}

// controller is a component that can take over management of endpoints
// resources corresponding to selector-less, Osiris-enabled services
type controller struct {
	kubeClient             kubernetes.Interface
	activatorPodsInformer  cache.SharedIndexInformer
	readyActivatorPods     map[string]corev1.Pod
	readyActivatorPodsLock sync.Mutex
	servicesInformer       cache.SharedIndexInformer
	managers               map[string]*endpointsManager
	managersLock           sync.Mutex
	ctx                    context.Context
}

// NewController returns a new component that can take over management of
// endpoints resources corresponding to selector-less, Osiris-enabled services
func NewController(
	config Config,
	kubeClient kubernetes.Interface,
) Controller {
	activatorPodsSelector := labels.SelectorFromSet(
		map[string]string{
			config.ActivatorPodLabelSelectorKey: config.ActivatorPodLabelSelectorValue, // nolint: lll
		},
	)
	c := &controller{
		kubeClient: kubeClient,
		activatorPodsInformer: k8s.PodsIndexInformer(
			kubeClient,
			config.OsirisNamespace,
			nil,
			activatorPodsSelector,
		),
		readyActivatorPods: map[string]corev1.Pod{},
		servicesInformer: k8s.ServicesIndexInformer(
			kubeClient,
			metav1.NamespaceAll,
			nil,
			nil,
		),
		managers: map[string]*endpointsManager{},
	}
	c.activatorPodsInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: c.syncActivatorPod,
			UpdateFunc: func(_, newObj interface{}) {
				c.syncActivatorPod(newObj)
			},
			DeleteFunc: c.syncActivatorPod,
		},
	)
	c.servicesInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: c.syncAppService,
			UpdateFunc: func(_, newObj interface{}) {
				c.syncAppService(newObj)
			},
			DeleteFunc: c.syncDeletedAppService,
		},
	)
	return c
}

// Run causes the controller to manage endpoints resources corresponding to
// selector-less, Osiris-enabled services. This function will not return until
// the context it has been passed expires or is canceled.
func (c *controller) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.ctx = ctx
	go func() {
		<-ctx.Done()
		glog.Infof("Controller is shutting down")
	}()
	glog.Infof("Controller is started")
	go func() {
		c.activatorPodsInformer.Run(ctx.Done())
		cancel()
	}()
	go func() {
		c.servicesInformer.Run(ctx.Done())
		cancel()
	}()
	healthz.RunServer(ctx, 5000)
	cancel()
}

// syncAppService is notified of all new and updated service resources. For
// those that are Osiris-enabled, on-going management of that service's
// corresponding endpoints resource will be guaranteed, whilst the same will
// be prevented for non-Osiris-enabled services.
func (c *controller) syncAppService(obj interface{}) {
	svc := obj.(*corev1.Service)
	if k8s.ResourceIsOsirisEnabled(svc.Annotations) {
		glog.Infof(
			"Notified about new or updated Osiris-enabled service %s in namespace %s",
			svc.Name,
			svc.Namespace,
		)
		c.ensureServiceEndpointsManaged(svc)
	} else {
		glog.Infof(
			"Notified about new or updated non-Osiris-enabled service %s in "+
				"namespace %s",
			svc.Name,
			svc.Namespace,
		)
		c.ensureServiceEndpointsNotManaged(svc)
	}
}

// syncDeletedAppService is notified of all deleted service resources. It
// ensures any on-going management (if any) of the service's corresponding
// endpoints resource is halted.
func (c *controller) syncDeletedAppService(obj interface{}) {
	svc := obj.(*corev1.Service)
	glog.Infof(
		"Notified about deleted service %s in namespace %s",
		svc.Name,
		svc.Namespace,
	)
	c.ensureServiceEndpointsNotManaged(svc)
}

// ensureServiceEndpointsManaged guarantees ongoing management of the specified
// service's corresponding endpoints resource. This is accomplished by launching
// a manager component, which itself is a controller that watches application
// pods to reify endpoints.
func (c *controller) ensureServiceEndpointsManaged(svc *corev1.Service) {
	c.managersLock.Lock()
	defer c.managersLock.Unlock()
	key := getServiceKey(svc)
	if e, ok := c.managers[key]; ok {
		e.stop()
		delete(c.managers, key)
	}
	// Whether net new or a replacement, it's time for a new manager...
	m, err := newEndpointsManager(svc, c)
	if err != nil {
		glog.Errorf(
			"Error creating endpoints manager for service %s in namespace %s: %s",
			svc.Name,
			svc.Namespace,
			err,
		)
		return
	}
	go m.run(c.ctx)
	c.managers[key] = m
}

// ensureServiceEndpointsNotManaged halts ongoing management (if any) of the
// specified service's corresponding endpoints resource
func (c *controller) ensureServiceEndpointsNotManaged(svc *corev1.Service) {
	c.managersLock.Lock()
	defer c.managersLock.Unlock()
	key := getServiceKey(svc)
	if e, ok := c.managers[key]; ok {
		// We're currently managing this service's endpoints. Let's stop!
		e.stop()
		delete(c.managers, key)
	}
}

// syncActivatorPod is notified of all changes to activator pods-- creates,
// updates, and deletes. Pods that are in a ready state (and ONLY pods that are
// in a ready state) are tracked in a map. This always-up-to-date set of ready
// activator pods is used to provide endpoints to any Osiris-enabled service
// that lacks application endpoints.
func (c *controller) syncActivatorPod(obj interface{}) {
	c.readyActivatorPodsLock.Lock()
	defer c.readyActivatorPodsLock.Unlock()
	pod := obj.(*corev1.Pod)
	var ready bool
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			if condition.Status == corev1.ConditionTrue {
				ready = true
			}
			break
		}
	}
	glog.Infof(
		"Informed about activator pod %s; its IP is %s and its ready "+
			"condition is %t",
		pod.Name,
		pod.Status.PodIP,
		ready,
	)
	if ready {
		c.readyActivatorPods[pod.Name] = *pod
	} else {
		delete(c.readyActivatorPods, pod.Name)
	}
	glog.Infof(
		"%d pods ready for activator",
		len(c.readyActivatorPods),
	)
	for _, mgr := range c.managers {
		func() {
			mgr.readyAppPodsLock.Lock()
			defer mgr.readyAppPodsLock.Unlock()
			mgr.syncEndpoints()
		}()
	}
}

// getServiceKey concatenates a service's namespace and name to form a key that
// is a suitably unique identifier for use as a key in a map of services to
// the managers that are minding each service's corresponding endpoints
// resource.
func getServiceKey(svc *corev1.Service) string {
	return fmt.Sprintf("%s:%s", svc.Namespace, svc.Name)
}
