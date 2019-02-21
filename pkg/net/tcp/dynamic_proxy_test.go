package tcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	mynet "github.com/deislabs/osiris/pkg/net"
	myhttp "github.com/deislabs/osiris/pkg/net/http"
	"github.com/phayes/freeport"
	"github.com/stretchr/testify/require"
)

func TestNewDynamicProxy(t *testing.T) {
	var l7StartCalled bool
	l7Start := func(*http.Request) (string, int, error) {
		l7StartCalled = true
		return "localhost", 5000, nil
	}
	var l7EndCalled bool
	l7End := func(*http.Request) error {
		l7EndCalled = true
		return nil
	}
	var l4StartCalled bool
	l4Start := func(string) (string, int, error) {
		l4StartCalled = true
		return "localhost", 5000, nil
	}
	var l4EndCalled bool
	l4End := func(string) error {
		l4EndCalled = true
		return nil
	}
	d, err := NewDynamicProxy(
		"localhost:5000",
		l7Start,
		l7End,
		l4Start,
		l4End,
	)
	require.NoError(t, err)
	dp, ok := d.(*dynamicProxy)
	require.True(t, ok)
	require.NotNil(t, dp.listenAddr)
	require.NotNil(t, dp.httpVersionFn)
	require.NotNil(t, dp.clientHelloServerNameFn)
	// Can't assert function equality, apparently, so to make sure all functions
	// are set correctly, we'll invoke each one and then check that it got called.
	_, _, err = dp.l7StartProxyCallback(nil)
	require.NoError(t, err)
	require.True(t, l7StartCalled)
	require.NotNil(t, dp.l7ProxyFn)
	err = dp.l7EndProxyCallback(nil)
	require.NoError(t, err)
	require.True(t, l7EndCalled)
	_, _, err = dp.l4StartProxyCallback("")
	require.NoError(t, err)
	require.True(t, l4StartCalled)
	require.NotNil(t, dp.l4ProxyFn)
	err = dp.l4EndProxyCallback("")
	require.NoError(t, err)
	require.True(t, l4EndCalled)
}

func TestListenAndServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listenPort, err := freeport.GetFreePort()
	require.NoError(t, err)
	listenAddr, err := net.ResolveTCPAddr(
		"tcp",
		fmt.Sprintf("localhost:%d", listenPort),
	)
	require.NoError(t, err)
	serveConnectionCalledCh := make(chan struct{})
	dp := &dynamicProxy{
		listenAddr: listenAddr,
		serveConnectionFn: func(net.Conn) error {
			close(serveConnectionCalledCh)
			return nil
		},
	}
	go func() {
		gerr := dp.ListenAndServe(ctx)
		require.NoError(t, gerr)
	}()
	_, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", listenPort))
	require.NoError(t, err)
	select {
	case <-serveConnectionCalledCh:
	case <-time.After(2 * time.Second):
		require.Fail(t, "timed out waiting for serveConnectionFn to be called")
	}
}

func TestDefaultServeConnection(t *testing.T) {
	testCases := []struct {
		name             string
		httpVersion      string
		shouldUseL7Proxy bool
		shouldUseL4Proxy bool
		errAssertion     func(*testing.T, error)
	}{
		{
			name:             "HTTP connection",
			httpVersion:      "1.1",
			shouldUseL7Proxy: true,
			shouldUseL4Proxy: false,
			errAssertion: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:             "TLS connection",
			httpVersion:      "",
			shouldUseL7Proxy: false,
			shouldUseL4Proxy: true,
			errAssertion: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:             "unrecognizable connection",
			httpVersion:      "",
			shouldUseL7Proxy: false,
			shouldUseL4Proxy: false,
			errAssertion: func(t *testing.T, err error) {
				require.Error(t, err)
				require.Equal(
					t,
					"Connection not recognized as being used for HTTP or TLS",
					err.Error(),
				)
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var l7ProxyUsed, l4ProxyUsed bool
			dp := &dynamicProxy{
				httpVersionFn: func(conn mynet.PeekableConn) string {
					return testCase.httpVersion
				},
				clientHelloServerNameFn: func(conn mynet.PeekableConn) string {
					if testCase.shouldUseL4Proxy {
						return "www.example.com"
					}
					return ""
				},
				l7ProxyFn: func(
					net.Conn,
					string,
					myhttp.L7StartProxyCallback,
					myhttp.L7EndProxyCallback,
				) error {
					l7ProxyUsed = true
					return nil
				},
				l4ProxyFn: func(
					net.Conn,
					string,
					l4StartProxyCallback,
					l4EndProxyCallback,
				) error {
					l4ProxyUsed = true
					return nil
				},
			}
			// Meh... this is just a convenient way to get a connection we can
			// play with. Open to it being done differently.
			clientConn, proxyConn := net.Pipe()
			defer clientConn.Close()
			defer proxyConn.Close()
			err := dp.defaultServeConnection(proxyConn)
			testCase.errAssertion(t, err)
			require.Equal(t, testCase.shouldUseL7Proxy, l7ProxyUsed)
			require.Equal(t, testCase.shouldUseL4Proxy, l4ProxyUsed)
		})
	}
}
