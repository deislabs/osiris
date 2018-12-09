package proxy

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
)

type singlePortProxy struct {
	targetAddress string
	appPort       int
	requestCount  *uint64
	srv           *http.Server
}

func newSinglePortProxy(
	proxyPort int,
	appPort int,
	requestCount *uint64,
) *singlePortProxy {
	mux := http.NewServeMux()
	s := &singlePortProxy{
		targetAddress: fmt.Sprintf("http://localhost:%d", appPort),
		appPort:       appPort,
		requestCount:  requestCount,
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", proxyPort),
			Handler: mux,
		},
	}
	mux.HandleFunc("/", s.handleRequest)
	return s
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

	req, err := http.NewRequest(r.Method, s.targetAddress, r.Body)
	if err != nil {
		glog.Errorf("Error creating outbound request: %s", err)
		s.returnError(w, http.StatusInternalServerError)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		glog.Errorf("Error executing outbound request: %s", err)
		s.returnError(w, http.StatusInternalServerError)
		return
	}

	body, _ := ioutil.ReadAll(resp.Body)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(body); err != nil {
		glog.Errorf("Error writing response body: %s", err)
	} else {
		glog.Info("Request sent")
	}
}

func (s *singlePortProxy) returnError(w http.ResponseWriter, statusCode int) {
	w.WriteHeader(statusCode)
	if _, err := w.Write([]byte{}); err != nil {
		glog.Errorf("Error writing response body: %s", err)
	}
}
