package http

import (
	"context"
	"crypto/tls"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deislabs/osiris/pkg/net/http/httputil"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestProxySingleHTTP1xConnection(t *testing.T) {
	// Set up a backend
	body := []byte("foobar")
	backend := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, err := w.Write(body)
				require.NoError(t, err)
			},
		),
	)
	defer backend.Close()

	// Use an in-memory connection pair
	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()
	defer proxyConn.Close()

	// Proxy the proxy end of the connection. This is the function under test.
	go func() {
		gerr := ProxySingleConnection(proxyConn, "1.1", nil, nil)
		require.NoError(t, gerr)
	}()

	// Use an HTTP client with a custom transport that uses the established
	// client connection.
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return clientConn, nil
			},
		},
	}

	// Make a request to the backend. Note this won't go DIRECTLY to the backend
	// since the remote end of the client connection (see custom transport above)
	// is handled by our proxy function.
	req, err := http.NewRequest("GET", backend.URL, nil)
	require.NoError(t, err)

	// Make a request
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert that the response is as if we connected directly to the backend.
	require.Equal(t, http.StatusOK, resp.StatusCode)
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, bodyBytes)
}

func TestProxySingleHTTP2xConnection(t *testing.T) {
	body := []byte("foobar")
	backend := httptest.NewServer(
		h2c.NewHandler(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, err := w.Write(body)
					require.NoError(t, err)
				},
			),
			&http2.Server{},
		),
	)
	defer backend.Close()

	// Use an in-memory connection pair
	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()
	defer proxyConn.Close()

	// Proxy the proxy end of the connection. This is the function under test.
	go func() {
		gerr := ProxySingleConnection(proxyConn, "2.0", nil, nil)
		require.NoError(t, gerr)
	}()

	// Use an HTTP client with a custom transport that uses the established
	// client connection.
	httpClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(netw, addr string, cfg *tls.Config) (net.Conn, error) {
				return clientConn, nil
			},
		},
	}

	// Make a request to the backend. Note this won't go DIRECTLY to the backend
	// since the remote end of the client connection (see custom transport above)
	// is handled by our proxy function.
	req, err := http.NewRequest("GET", backend.URL, nil)
	require.NoError(t, err)

	// Make a request
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert that the response is as if we connected directly to the backend.
	require.Equal(t, http.StatusOK, resp.StatusCode)
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, bodyBytes)
}

func TestServeHTTP1x(t *testing.T) {
	body := []byte("foobar")
	var startProxyCallbackCalled, endProxyCallbackCalled bool
	handler := &http1xProxyRequestHandler{
		startProxyCallback: func(r *http.Request) (string, int, error) {
			startProxyCallbackCalled = true
			// We know the host is www.example.com with no port specified-- i.e. 80
			return r.Host, 80, nil
		},
		proxyRequestFn: func(
			w http.ResponseWriter,
			r *http.Request,
			h http.Handler,
		) {
			require.IsType(t, &httputil.ReverseProxy{}, h)
			w.WriteHeader(http.StatusOK)
			_, err := w.Write(body)
			require.NoError(t, err)
		},
		endProxyCallback: func(*http.Request) error {
			endProxyCallbackCalled = true
			return nil
		},
		doneCh: make(chan struct{}),
	}
	req, err := http.NewRequest("GET", "/foo", nil)
	require.NoError(t, err)
	req.Header.Set("Host", "www.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, body, rr.Body.Bytes())
	require.True(t, startProxyCallbackCalled)
	require.True(t, endProxyCallbackCalled)
}

func TestServeHTTP2x(t *testing.T) {
	body := []byte("foobar")
	var startProxyCallbackCalled, endProxyCallbackCalled bool
	handler := &http2xProxyRequestHandler{
		startProxyCallback: func(r *http.Request) (string, int, error) {
			startProxyCallbackCalled = true
			// We know the host is www.example.com with no port specified-- i.e. 80
			return r.Host, 80, nil
		},
		proxyRequestFn: func(
			w http.ResponseWriter,
			r *http.Request,
			h http.Handler,
		) {
			require.IsType(t, &httputil.ReverseProxy{}, h)
			w.WriteHeader(http.StatusOK)
			_, err := w.Write(body)
			require.NoError(t, err)
		},
		endProxyCallback: func(*http.Request) error {
			endProxyCallbackCalled = true
			return nil
		},
	}
	req, err := http.NewRequest("GET", "/foo", nil)
	require.NoError(t, err)
	req.Header.Set("Host", "www.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, body, rr.Body.Bytes())
	require.True(t, startProxyCallbackCalled)
	require.True(t, endProxyCallbackCalled)
}

func TestDefaultProxyRequest(t *testing.T) {
	var handlerCalled bool
	handler := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
		},
	)
	defaultProxyRequest(nil, nil, handler)
	require.True(t, handlerCalled)
}
