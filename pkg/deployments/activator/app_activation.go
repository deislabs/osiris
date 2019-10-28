package activator

import (
	"context"
	"sync"
	"time"

	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type appActivation struct {
	readyAppPodIPs map[string]struct{}
	endpoints      *corev1.Endpoints
	lock           sync.Mutex
	successCh      chan struct{}
	timeoutCh      chan struct{}
}

func (a *appActivation) watchForCompletion(
	kubeClient kubernetes.Interface,
	app *app,
	appPodSelector labels.Selector,
) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Watch the pods managed by this deployment/statefulSet
	podsInformer := k8s.PodsIndexInformer(
		kubeClient,
		app.namespace,
		nil,
		appPodSelector,
	)
	podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: a.syncPod,
		UpdateFunc: func(_, newObj interface{}) {
			a.syncPod(newObj)
		},
		DeleteFunc: a.syncPod,
	})
	// Watch the corresponding endpoints resource for this service
	endpointsInformer := k8s.EndpointsIndexInformer(
		kubeClient,
		app.namespace,
		fields.OneTermEqualSelector(
			"metadata.name",
			app.serviceName,
		),
		nil,
	)
	endpointsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: a.syncEndpoints,
		UpdateFunc: func(_, newObj interface{}) {
			a.syncEndpoints(newObj)
		},
	})
	go podsInformer.Run(ctx.Done())
	go endpointsInformer.Run(ctx.Done())
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-a.successCh:
			return
		case <-timer.C:
			glog.Errorf(
				"Activation of %s %s in namespace %s timed out",
				app.kind,
				app.name,
				app.namespace,
			)
			close(a.timeoutCh)
			return
		}
	}
}

func (a *appActivation) syncPod(obj interface{}) {
	a.lock.Lock()
	defer a.lock.Unlock()
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
	// Keep track of which pods are ready
	if ready {
		a.readyAppPodIPs[pod.Status.PodIP] = struct{}{}
	} else {
		delete(a.readyAppPodIPs, pod.Status.PodIP)
	}
	a.checkActivationComplete()
}

func (a *appActivation) syncEndpoints(obj interface{}) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.endpoints = obj.(*corev1.Endpoints)
	a.checkActivationComplete()
}

func (a *appActivation) checkActivationComplete() {
	if a.endpoints != nil {
		for _, subset := range a.endpoints.Subsets {
			for _, address := range subset.Addresses {
				if _, ok := a.readyAppPodIPs[address.IP]; ok {
					glog.Infof("App pod with ip %s is in service", address.IP)
					close(a.successCh)
					return
				}
			}
		}
	}
}
