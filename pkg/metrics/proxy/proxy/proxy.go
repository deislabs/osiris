package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/deislabs/osiris/pkg/healthz"
	"github.com/deislabs/osiris/pkg/metrics"
	"github.com/deislabs/osiris/pkg/net/tcp"
	"github.com/golang/glog"
	uuid "github.com/satori/go.uuid"
)

type Proxy interface {
	Run(ctx context.Context)
}

type proxy struct {
	proxyID              string
	connectionsOpened    *uint64
	connectionsClosed    *uint64
	dynamicProxies       []tcp.DynamicProxy
	healthzAndMetricsSvr *http.Server
	ignoredPaths         map[string]struct{}
}

func NewProxy(cfg Config) (Proxy, error) {
	var connectionsOpened, connectionsClosed uint64
	healthzAndMetricsMux := http.NewServeMux()
	p := &proxy{
		proxyID:           uuid.NewV4().String(),
		connectionsOpened: &connectionsOpened,
		connectionsClosed: &connectionsClosed,
		dynamicProxies:    []tcp.DynamicProxy{},
		healthzAndMetricsSvr: &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.MetricsAndHealthPort),
			Handler: healthzAndMetricsMux,
		},
		ignoredPaths: cfg.IgnoredPaths,
	}

	for listenPort, targetPort := range cfg.PortMappings {
		tp := targetPort
		dynamicProxy, err := tcp.NewDynamicProxy(
			fmt.Sprintf(":%d", listenPort),
			func(r *http.Request) (string, int, error) {
				if !p.isIgnoredRequest(r) {
					atomic.AddUint64(p.connectionsOpened, 1)
				}
				return "localhost", tp, nil
			},
			func(r *http.Request) error {
				if !p.isIgnoredRequest(r) {
					atomic.AddUint64(p.connectionsClosed, 1)
				}
				return nil
			},
			func(string) (string, int, error) {
				atomic.AddUint64(p.connectionsOpened, 1)
				return "localhost", tp, nil
			},
			func(string) error {
				atomic.AddUint64(p.connectionsClosed, 1)
				return nil
			},
		)
		if err != nil {
			return nil, err
		}
		p.dynamicProxies = append(
			p.dynamicProxies,
			dynamicProxy,
		)
	}
	healthzAndMetricsMux.HandleFunc("/metrics", p.handleMetricsRequest)
	healthzAndMetricsMux.HandleFunc("/healthz", healthz.HandleHealthCheckRequest)
	return p, nil
}

func (p *proxy) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)

	// Start proxies for each port
	for _, dp := range p.dynamicProxies {
		go func(dp tcp.DynamicProxy) {
			if err := dp.ListenAndServe(ctx); err != nil {
				glog.Errorf("Error listening and serving: %s", err)
			}
			cancel()
		}(dp)
	}

	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		select {
		case <-ctx.Done(): // Context was canceled or expired
			glog.Info("Healthz and metrics server is shutting down")
			// Allow up to five seconds for requests in progress to be completed
			shutdownCtx, shutdownCancel := context.WithTimeout(
				context.Background(),
				time.Second*5,
			)
			defer shutdownCancel()
			p.healthzAndMetricsSvr.Shutdown(shutdownCtx) // nolint: errcheck
		case <-doneCh: // The server shut down on its own, perhaps due to error
		}
		cancel()
	}()

	glog.Infof(
		"Healthz and metrics server is listening on %s",
		p.healthzAndMetricsSvr.Addr,
	)
	err := p.healthzAndMetricsSvr.ListenAndServe()
	if err != http.ErrServerClosed {
		glog.Errorf("Error from healthz and metrics server: %s", err)
	}
}

func (p *proxy) handleMetricsRequest(w http.ResponseWriter, _ *http.Request) {
	pcs := metrics.ProxyConnectionStats{
		ProxyID:           p.proxyID,
		ConnectionsOpened: atomic.LoadUint64(p.connectionsOpened),
		ConnectionsClosed: atomic.LoadUint64(p.connectionsClosed),
	}
	pcsBytes, err := json.Marshal(pcs)
	if err != nil {
		glog.Errorf("Error marshaling metrics request response: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(pcsBytes); err != nil {
		glog.Errorf("Error writing metrics request response body: %s", err)
	}
}

func (p *proxy) isIgnoredRequest(r *http.Request) bool {
	return p.isIgnoredPath(r) || isKubeProbe(r)
}

func (p *proxy) isIgnoredPath(r *http.Request) bool {
	if r.URL == nil || len(r.URL.Path) == 0 {
		return false
	}
	_, found := p.ignoredPaths[r.URL.Path]
	return found
}

func isKubeProbe(r *http.Request) bool {
	return strings.Contains(r.Header.Get("User-Agent"), "kube-probe")
}
