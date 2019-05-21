package http

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/deislabs/osiris/pkg/net/http/httputil"
	"github.com/golang/glog"
	"golang.org/x/net/http2"
)

// L7StartProxyCallback is the function signature for functions used as
// callbacks before an L7 proxy starts.
type L7StartProxyCallback func(*http.Request) (string, int, error)

// L7EndProxyCallback is the function signature for functions used as
// callbacks after an L7 proxy completes.
type L7EndProxyCallback func(*http.Request) error

// ProxySingleConnection constructs an HTTP reverse proxy capable of proxying
// both HTTP 1.x and h2c (HTTP/2 without TLS) requests and uses it to serve the
// provided connection.
func ProxySingleConnection(
	conn net.Conn,
	httpVersion string,
	startProxyCallback L7StartProxyCallback,
	endProxyCallback L7EndProxyCallback,
) error {
	switch httpVersion {
	case "1.0", "1.1":
		doneCh := make(chan struct{})
		server := &http.Server{
			Handler: &http1xProxyRequestHandler{
				proxyRequestFn:     defaultProxyRequest,
				startProxyCallback: startProxyCallback,
				endProxyCallback:   endProxyCallback,
				doneCh:             doneCh,
			},
		}
		if err := server.Serve(
			&singleConnectionListener{
				conn: conn,
			},
		); err != nil && err != io.EOF {
			// EOF is normal when the singleConnectionListener tries to accept a
			// second connection.
			return fmt.Errorf("Error serving connection: %s", err)
		}
		// Wait for the handler to be done
		<-doneCh
		return nil
	case "2.0":
		server := http2.Server{}
		server.ServeConn(conn, &http2.ServeConnOpts{
			Handler: &http2xProxyRequestHandler{
				proxyRequestFn:     defaultProxyRequest,
				startProxyCallback: startProxyCallback,
				endProxyCallback:   endProxyCallback,
			},
		})
		return nil
	default:
		return fmt.Errorf("Unrecognized HTTP version %s", httpVersion)
	}
}

// http1xProxyRequestHandler is used internally to handle all of the HTTP
// proxy's inbound HTTP/1.x requests.
type http1xProxyRequestHandler struct {
	// This can be overridden for test purposes
	proxyRequestFn func(
		http.ResponseWriter,
		*http.Request,
		http.Handler,
	)
	startProxyCallback L7StartProxyCallback
	endProxyCallback   L7EndProxyCallback
	doneCh             chan struct{}
}

// ServeHTTP handles all of the HTTP proxy's inbound requests. It looks at the
// protocol's major version to distinguish between HTTP 1.x and h2c (HTTP/2
// without TLS) requests and also looks at the HTTP host header. Using these
// details, it constructs an appropriate httputil.NewSingleHostReverseProxy and
// hands off the request.
func (h *http1xProxyRequestHandler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	targetHost := r.Host
	if h.startProxyCallback != nil {
		th, tp, err := h.startProxyCallback(r)
		if err != nil {
			glog.Errorf(
				"Error executing start proxy callback for host \"%s\": %s",
				r.Host,
				err,
			)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		targetHost = fmt.Sprintf("%s:%d", th, tp)
	}
	targetURL, err := url.Parse(fmt.Sprintf("http://%s", targetHost))
	if err != nil {
		glog.Errorf("Error parsing target URL for host \"%s\": %s", targetHost, err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	h.proxyRequestFn(w, r, proxy)
	if h.endProxyCallback != nil {
		if err := h.endProxyCallback(r); err != nil {
			glog.Errorf(
				"Error executing end proxy callback for host \"%s\": %s",
				r.Host,
				err,
			)
		}
	}
	close(h.doneCh)
}

// http2xProxyRequestHandler is used internally to handle all of the HTTP
// proxy's inbound HTTP/2.x requests.
type http2xProxyRequestHandler struct {
	// This can be overridden for test purposes
	proxyRequestFn func(
		http.ResponseWriter,
		*http.Request,
		http.Handler,
	)
	startProxyCallback L7StartProxyCallback
	endProxyCallback   L7EndProxyCallback
}

// ServeHTTP handles all of the HTTP proxy's inbound requests. It looks at the
// protocol's major version to distinguish between HTTP 1.x and h2c (HTTP/2
// without TLS) requests and also looks at the HTTP host header. Using these
// details, it constructs an appropriate httputil.NewSingleHostReverseProxy and
// hands off the request.
func (h *http2xProxyRequestHandler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	targetHost := r.Host
	if h.startProxyCallback != nil {
		th, tp, err := h.startProxyCallback(r)
		if err != nil {
			glog.Errorf(
				"Error executing start proxy callback for host \"%s\": %s",
				r.Host,
				err,
			)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		targetHost = fmt.Sprintf("%s:%d", th, tp)
	}
	targetURL, err := url.Parse(fmt.Sprintf("http://%s", targetHost))
	if err != nil {
		glog.Errorf("Error parsing target URL for host \"%s\": %s", targetHost, err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = h2cDefaultTransport
	h.proxyRequestFn(w, r, proxy)
	if h.endProxyCallback != nil {
		if err := h.endProxyCallback(r); err != nil {
			glog.Errorf(
				"Error executing end proxy callback for host \"%s\": %s",
				r.Host,
				err,
			)
		}
	}
}

func defaultProxyRequest(
	w http.ResponseWriter,
	r *http.Request,
	proxy http.Handler,
) {
	proxy.ServeHTTP(w, r)
}
