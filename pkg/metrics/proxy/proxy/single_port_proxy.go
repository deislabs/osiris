package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
)

type singlePortProxy struct {
	appPort             int
	requestCount        *uint64
	srv                 *http.Server
	proxyRequestHandler *httputil.ReverseProxy
}

func newSinglePortProxy(
	proxyPort int,
	appPort int,
	requestCount *uint64,
) (*singlePortProxy, error) {
	targetURL, err := url.Parse(fmt.Sprintf("http://localhost:%d", appPort))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	s := &singlePortProxy{
		appPort:      appPort,
		requestCount: requestCount,
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", proxyPort),
			Handler: mux,
		},
		proxyRequestHandler: httputil.NewSingleHostReverseProxy(targetURL),
	}
	mux.HandleFunc("/", s.handleRequest)
	return s, nil
}

func (s *singlePortProxy) run(ctx context.Context) {
	doneCh := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done(): // Context was canceled or expired
			glog.Infof(
				"Proxy listening on %s proxying application port %d is shutting down",
				s.srv.Addr,
				s.appPort,
			)
			// Allow up to five seconds for requests in progress to be completed
			shutdownCtx, cancel := context.WithTimeout(
				context.Background(),
				time.Second*5,
			)
			defer cancel()
			s.srv.Shutdown(shutdownCtx) // nolint: errcheck
		case <-doneCh: // The server shut down on its own, perhaps due to error
		}
	}()

	glog.Infof(
		"Proxy listening on %s is proxying application port %d",
		s.srv.Addr,
		s.appPort,
	)
	err := s.srv.ListenAndServe()
	if err != http.ErrServerClosed {
		glog.Errorf(
			"Error from proxy listening on %s is proxying application port %d: %s",
			s.srv.Addr,
			s.appPort,
			err,
		)
	}
	close(doneCh)
}

func (s *singlePortProxy) handleRequest(
	w http.ResponseWriter,
	r *http.Request,
) {
	defer r.Body.Close()

	// We ensure that kubelet requests like health checks
	// are not instrumented by the sidecar proxy.
	userAgent := r.Header.Get("User-Agent")
	if !strings.Contains(userAgent, "kube-probe") {
		atomic.AddUint64(s.requestCount, 1)
	}

	s.proxyRequestHandler.ServeHTTP(w, r)
}
