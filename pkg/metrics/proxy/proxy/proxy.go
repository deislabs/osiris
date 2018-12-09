package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/deislabs/osiris/pkg/healthz"
	"github.com/deislabs/osiris/pkg/metrics"
	"github.com/golang/glog"
	uuid "github.com/satori/go.uuid"
)

type Proxy interface {
	Run(ctx context.Context)
}

type proxy struct {
	proxyID              string
	requestCount         *uint64
	singlePortProxies    []*singlePortProxy
	healthzAndMetricsSvr *http.Server
}

func NewProxy(cfg Config) Proxy {
	var requestCount uint64
	healthzAndMetricsMux := http.NewServeMux()
	p := &proxy{
		proxyID:           uuid.NewV4().String(),
		requestCount:      &requestCount,
		singlePortProxies: []*singlePortProxy{},
		healthzAndMetricsSvr: &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.MetricsAndHealthPort),
			Handler: healthzAndMetricsMux,
		},
	}
	for proxyPort, appPort := range cfg.PortMappings {
		p.singlePortProxies = append(
			p.singlePortProxies,
			newSinglePortProxy(proxyPort, appPort, p.requestCount),
		)
	}
	healthzAndMetricsMux.HandleFunc("/metrics", p.handleMetricsRequest)
	healthzAndMetricsMux.HandleFunc("/healthz", healthz.HandleHealthCheckRequest)
	return p
}

func (p *proxy) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)

	// Start proxies for each port
	for _, spp := range p.singlePortProxies {
		go func(spp *singlePortProxy) {
			spp.run(ctx)
			cancel()
		}(spp)
	}

	doneCh := make(chan struct{})

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
	close(doneCh)
}

func (p *proxy) handleMetricsRequest(w http.ResponseWriter, _ *http.Request) {
	prc := metrics.ProxyRequestCount{
		ProxyID:      p.proxyID,
		RequestCount: atomic.LoadUint64(p.requestCount),
	}
	prcBytes, err := json.Marshal(prc)
	if err != nil {
		glog.Errorf("Error marshaling metrics request response: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(prcBytes); err != nil {
		glog.Errorf("Error writing metrics request response body: %s", err)
	}
}
