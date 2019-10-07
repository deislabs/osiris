package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/deislabs/osiris/pkg/kubernetes"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
	endpointsv1 "k8s.io/kubernetes/pkg/api/v1/endpoints"
)

// endpointsManager is a controller responsible for the on-going management of
// the endpoints resource corresponding to a single Osiris-enabled service
type endpointsManager struct {
	service          corev1.Service
	podsInformer     cache.SharedIndexInformer
	controller       *controller
	readyAppPods     map[string]corev1.Pod
	readyAppPodsLock sync.Mutex
	cancelFunc       func()
}

// newEndpointsManager returns a new component that can provide on-going
// management of the endpoints resource corresponding to a single Osiris-enabled
// service
func newEndpointsManager(
	svc *corev1.Service,
	c *controller,
) (*endpointsManager, error) {
	encodedPodSelector, ok := svc.Annotations["osiris.deislabs.io/selector"]
	if !ok {
		return nil, fmt.Errorf(
			"Selector not found for service %s in namespace %s",
			svc.Name,
			svc.Namespace,
		)
	}
	selectorJSONBytes, err := base64.StdEncoding.DecodeString(encodedPodSelector)
	if err != nil {
		return nil, fmt.Errorf(
			"Error decoding selector for service %s in namespace %s: %s",
			svc.Name,
			svc.Namespace,
			err,
		)
	}
	selectorMap := map[string]string{}
	err = json.Unmarshal(selectorJSONBytes, &selectorMap)
	if err != nil {
		return nil, fmt.Errorf(
			"Error unmarshaling selector for service %s in namespace %s: %s",
			svc.Name,
			svc.Namespace,
			err,
		)
	}
	e := &endpointsManager{
		service: *svc,
		podsInformer: kubernetes.PodsIndexInformer(
			c.kubeClient,
			svc.Namespace,
			nil,
			labels.SelectorFromSet(selectorMap),
		),
		controller:   c,
		readyAppPods: map[string]corev1.Pod{},
	}
	e.podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: e.syncAppPod,
		UpdateFunc: func(_, newObj interface{}) {
			e.syncAppPod(newObj)
		},
		DeleteFunc: e.syncDeletedAppPod,
	})
	return e, nil
}

// run causes the manager to manage endpoints resources corresponding to
// a single Osiris-enabled service. This function will not return until the
// context it has been passed expires or is canceled.
func (e *endpointsManager) run(ctx context.Context) {
	ctx, e.cancelFunc = context.WithCancel(ctx)
	go func() {
		<-ctx.Done()
		glog.Infof(
			"Stopping endpoints management for service %s in namespace %s",
			e.service.Name,
			e.service.Namespace,
		)
	}()
	e.podsInformer.Run(ctx.Done())
	// force an initial sync of the endpoints for deployments that are initially
	// scaled to 0, and for which we won't see Pod events.
	e.syncEndpoints()
}

func (e *endpointsManager) stop() {
	e.cancelFunc()
}

// syncAppPod is notified of all changes to pods that WOULD have been selected
// by the Osiris-enabled service if it were not selector-less. Pods that are in
// a ready state (and ONLY pods that are in a ready state) are tracked in a map.
// This always-up-to-date set of ready application pods is used to provide
// endpoints to the Osiris-enabled service.
func (e *endpointsManager) syncAppPod(obj interface{}) {
	e.controller.readyActivatorPodsLock.Lock()
	defer e.controller.readyActivatorPodsLock.Unlock()
	e.readyAppPodsLock.Lock()
	defer e.readyAppPodsLock.Unlock()
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
		"Informed about pod %s for service %s in namespace %s; its IP is %s and "+
			"its ready condition is %t",
		pod.Name,
		e.service.Name,
		e.service.Namespace,
		pod.Status.PodIP,
		ready,
	)
	if ready {
		e.readyAppPods[pod.Name] = *pod
	} else {
		delete(e.readyAppPods, pod.Name)
	}
	glog.Infof(
		"%d app pods ready for service %s in namespace %s",
		len(e.readyAppPods),
		e.service.Name,
		e.service.Namespace,
	)
	e.syncEndpoints()
}

func (e *endpointsManager) syncDeletedAppPod(obj interface{}) {
	e.controller.readyActivatorPodsLock.Lock()
	defer e.controller.readyActivatorPodsLock.Unlock()
	e.readyAppPodsLock.Lock()
	defer e.readyAppPodsLock.Unlock()
	pod := obj.(*corev1.Pod)
	glog.Infof(
		"Informed about deleted pod %s for service %s in namespace %s",
		pod.Name,
		e.service.Name,
		e.service.Namespace,
	)
	delete(e.readyAppPods, pod.Name)
	e.syncEndpoints()
}

// syncEndpoints is a helper function invoked whenever other changes necessitate
// a refresh of the endpoints object corresponding to the Osiris-enabled
// service.
func (e *endpointsManager) syncEndpoints() {
	subsets := []corev1.EndpointSubset{}
	for _, servicePort := range e.service.Spec.Ports {
		var foundSuitableAppPod bool
		for _, appPod := range e.readyAppPods {
			appPodPort, ok := findPodPort(appPod, servicePort)
			if !ok {
				continue
			}
			foundSuitableAppPod = true
			subsets = append(subsets, corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					{
						IP: appPod.Status.PodIP,
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Name:     servicePort.Name,
						Port:     appPodPort,
						Protocol: servicePort.Protocol,
					},
				},
			})
		}
		if !foundSuitableAppPod {
			// None of the ready pods expose a back end service for this service's
			// port. i.e. There are no endpoints. Add activator endpoints instead.
			for _, proxyPod := range e.controller.readyActivatorPods {
				subsets = append(subsets, corev1.EndpointSubset{
					Addresses: []corev1.EndpointAddress{
						{
							IP: proxyPod.Status.PodIP,
						},
					},
					Ports: []corev1.EndpointPort{
						{
							Name:     servicePort.Name,
							Port:     5000, // TODO: Maybe don't hard-code this?
							Protocol: servicePort.Protocol,
						},
					},
				})
			}
		}
	}
	subsets = endpointsv1.RepackSubsets(subsets)

	glog.Infof(
		"Creating or updating endpoints object for service %s in namespace %s",
		e.service.Name,
		e.service.Namespace,
	)
	if _, err := e.controller.kubeClient.CoreV1().Endpoints(
		e.service.Namespace,
	).Update(
		&corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      e.service.Name,
				Namespace: e.service.Namespace,
			},
			Subsets: subsets,
		},
	); err != nil {
		glog.Errorf(
			"Error creating or updating endpoints object for service %s in "+
				"namespace %s: %s",
			e.service.Name,
			e.service.Namespace,
			err,
		)
	}
}

// findPodPort locates the specific port for a given pod that provides an
// endpoint for the given servicePort, if such a port exists among any of the
// pod's containers
func findPodPort(pod corev1.Pod, svcPort corev1.ServicePort) (int32, bool) {
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if svcPort.TargetPort.Type == intstr.Int {
				if port.ContainerPort == svcPort.TargetPort.IntVal {
					return port.ContainerPort, true
				}
			} else {
				if port.Name == svcPort.TargetPort.StrVal {
					return port.ContainerPort, true
				}
			}
		}
	}
	return 0, false
}
