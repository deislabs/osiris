package activator

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/deislabs/osiris/pkg/healthz"
	k8s "github.com/deislabs/osiris/pkg/kubernetes"
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
	srv                       *http.Server
	httpClient                *http.Client
}

func NewActivator(kubeClient kubernetes.Interface) Activator {
	const port = 5000
	mux := http.NewServeMux()
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
		services:      map[string]*corev1.Service{},
		nodeAddresses: map[string]struct{}{},
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		},
		appsByHost:            map[string]*app{},
		deploymentActivations: map[string]*deploymentActivation{},
		httpClient: &http.Client{
			Timeout: time.Minute * 1,
		},
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
	mux.HandleFunc("/", a.handleRequest)
	return a
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
		if err := a.runServer(ctx); err != nil {
			glog.Errorf("Server error: %s", err)
			cancel()
		}
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
