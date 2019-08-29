package tcp

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/phayes/freeport"
	"github.com/stretchr/testify/require"
)

func TestDefaultProxyConnection(t *testing.T) {
	reqBytes := []byte("foo")
	respBytes := []byte("bar")

	// Set up a backend that will receive some bytes and send some back.
	backendPort, err := freeport.GetFreePort()
	require.NoError(t, err)
	backendAddr, err := net.ResolveTCPAddr(
		"tcp",
		fmt.Sprintf("localhost:%d", backendPort),
	)
	require.NoError(t, err)
	backendListener, err := net.ListenTCP("tcp", backendAddr)
	require.NoError(t, err)
	go func() {
		conn, gerr := backendListener.AcceptTCP()
		defer conn.Close()
		require.NoError(t, gerr)
		bytes := make([]byte, 1024)
		n, gerr := conn.Read(bytes)
		require.NoError(t, gerr)
		require.Equal(t, reqBytes, bytes[:n])
		n, gerr = conn.Write(respBytes)
		require.NoError(t, gerr)
		require.Equal(t, len(respBytes), n)
	}()

	// Use an in-memory connection pair for a TCP client and the proxy
	clientConn, proxyConn := net.Pipe()
	defer clientConn.Close()
	defer proxyConn.Close()

	// These are the bytes the client will write
	go func() {
		n, gerr := clientConn.Write(reqBytes)
		require.NoError(t, gerr)
		require.Equal(t, len(reqBytes), n)
	}()

	// Proxy the connection. This is the function under test.
	errCh := make(chan error)
	go func() {
		errCh <- defaultProxyConnection(
			proxyConn,
			"localhost",
			func(string) (string, int, error) {
				return "localhost", backendPort, nil
			},
			nil,
		)
	}()

	bytes := make([]byte, 1024)
	n, err := clientConn.Read(bytes)
	require.NoError(t, err)
	require.Equal(t, respBytes, bytes[:n])

	// Close the connection
	require.NoError(t, clientConn.Close())

	select {
	case err = <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		require.Fail(t, "timed out waiting for defaultProxyConnection() to return")
	}
}
