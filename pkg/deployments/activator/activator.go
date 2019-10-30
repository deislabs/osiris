package activator

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/deislabs/osiris/pkg/healthz"
	k8s "github.com/deislabs/osiris/pkg/kubernetes"
	"github.com/deislabs/osiris/pkg/net/tcp"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Activator interface {
	Run(ctx context.Context)
}

type activator struct {
	kubeClient                kubernetes.Interface
	servicesInformer          cache.SharedIndexInformer
	nodeInformer              cache.SharedIndexInformer
	services                  map[string]*corev1.Service
	nodeAddresses             map[string]struct{}
	appsByHost                map[string]*app
	indicesLock               sync.RWMutex
	deploymentActivations     map[string]*deploymentActivation
	deploymentActivationsLock sync.Mutex
	dynamicProxyListenAddrStr string
	dynamicProxy              tcp.DynamicProxy
}

func NewActivator(kubeClient kubernetes.Interface) (Activator, error) {
	const port = 5000
	a := &activator{
		kubeClient: kubeClient,
		servicesInformer: k8s.ServicesIndexInformer(
			kubeClient,
			metav1.NamespaceAll,
			nil,
			nil,
		),
		nodeInformer: k8s.NodesIndexInformer(
			kubeClient,
			metav1.NamespaceAll,
			nil,
			nil,
		),
		dynamicProxyListenAddrStr: fmt.Sprintf(":%d", port),
		services:                  map[string]*corev1.Service{},
		nodeAddresses:             map[string]struct{}{},
		appsByHost:                map[string]*app{},
		deploymentActivations:     map[string]*deploymentActivation{},
	}
	var err error
	a.dynamicProxy, err = tcp.NewDynamicProxy(
		a.dynamicProxyListenAddrStr,
		func(r *http.Request) (string, int, error) {
			return a.activateAndWait(r.Host)
		},
		nil,
		func(serverName string) (string, int, error) {
			return a.activateAndWait(fmt.Sprintf("%s:tls", serverName))
		},
		nil,
	)
	if err != nil {
		return nil, err
	}
	a.servicesInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: a.syncService,
		UpdateFunc: func(_, newObj interface{}) {
			a.syncService(newObj)
		},
		DeleteFunc: a.syncDeletedService,
	})
	a.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: a.syncNode,
		UpdateFunc: func(_, newObj interface{}) {
			a.syncNode(newObj)
		},
		DeleteFunc: a.syncDeletedNode,
	})
	return a, nil
}

func (a *activator) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-ctx.Done()
		glog.Infof("Activator is shutting down")
	}()
	glog.Infof("Activator is started")
	go func() {
		a.servicesInformer.Run(ctx.Done())
		cancel()
	}()
	go func() {
		a.nodeInformer.Run(ctx.Done())
		cancel()
	}()
	go func() {
		glog.Infof(
			"Activator server is listening on %s, proxying all deactivated, "+
				"Osiris-enabled applications",
			a.dynamicProxyListenAddrStr,
		)
		if err := a.dynamicProxy.ListenAndServe(ctx); err != nil {
			glog.Errorf("Error listening and serving: %s", err)
		}
		cancel()
	}()
	healthz.RunServer(ctx, 5001)
	cancel()
}

func (a *activator) syncService(obj interface{}) {
	a.indicesLock.Lock()
	defer a.indicesLock.Unlock()
	svc := obj.(*corev1.Service)
	svcKey := getKey(svc.Namespace, svc.Name)
	if k8s.ResourceIsOsirisEnabled(svc.Annotations) {
		a.services[svcKey] = svc
	} else {
		delete(a.services, svcKey)
	}
	a.updateIndex()
}

func (a *activator) syncDeletedService(obj interface{}) {
	a.indicesLock.Lock()
	defer a.indicesLock.Unlock()
	svc := obj.(*corev1.Service)
	svcKey := getKey(svc.Namespace, svc.Name)
	delete(a.services, svcKey)
	a.updateIndex()
}

func (a *activator) syncNode(obj interface{}) {
	a.indicesLock.Lock()
	defer a.indicesLock.Unlock()
	node := obj.(*corev1.Node)
	for _, nodeAddress := range node.Status.Addresses {
		a.nodeAddresses[nodeAddress.Address] = struct{}{}
	}
	a.updateIndex()
}

func (a *activator) syncDeletedNode(obj interface{}) {
	a.indicesLock.Lock()
	defer a.indicesLock.Unlock()
	node := obj.(*corev1.Node)
	for _, nodeAddress := range node.Status.Addresses {
		delete(a.nodeAddresses, nodeAddress.Address)
	}
	a.updateIndex()
}
